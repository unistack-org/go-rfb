// Package proxy provides a transport-agnostic VNC proxy built on plain
// net.Conn values.
//
// One side of the proxy is a standard RFB server (any VNC client can
// connect to it, e.g. noVNC behind a WebSocket->net.Conn adapter that the
// caller wires up themselves). The other side is an RFB client connected
// to an ATEN iKVM session. Hermon-encoded frames from the iKVM are
// transcoded on the fly into standard RFB rectangles, and for clients
// that advertised the tightpng encoding the rectangles are re-encoded as
// tight (PNG / Fill) so such clients receive a normal tight stream.
//
// The package deliberately does not contain any WebSocket code: the
// consumer decides how a WebSocket becomes a net.Conn. The only
// dependency is the parent rfb package.
package proxy

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	vnc "github.com/unistack-org/go-rfb"
)

// Dialer establishes the upstream (ATEN iKVM) connection.
type Dialer func(network, addr string) (net.Conn, error)

// Config configures a Proxy.
type Config struct {
	// UpstreamHost is the address of the ATEN iKVM RFB endpoint, e.g.
	// "192.168.1.50:5900".
	UpstreamHost string

	// UpstreamUser/UpstreamPassword are the ATEN iKVM credentials used
	// for the upstream authentication challenge.
	UpstreamUser     string
	UpstreamPassword string

	// Dialer is the function used to open the upstream connection.
	// Defaults to net.DialTimeout with a 10s timeout.
	Dialer Dialer

	// Upstream is an optional pre-established upstream connection. When
	// set, it takes precedence over UpstreamHost/Dialer.
	Upstream net.Conn

	// DownstreamSecurity is the security handler advertised to
	// downstream VNC clients. Defaults to no authentication.
	DownstreamSecurity vnc.SecurityHandler

	// DownstreamName is the desktop name shown to downstream clients.
	// Defaults to "ATEN iKVM".
	DownstreamName string

	// KeepAliveInterval is how often a no-op full-screen incremental
	// framebuffer update request is sent upstream to keep the iKVM
	// session (and idle TCP path) alive. Zero disables keepalives.
	// Defaults to 30s.
	KeepAliveInterval time.Duration
}

// Proxy bridges downstream RFB clients to a single upstream ATEN iKVM
// session. A Proxy is created by calling New.
type Proxy struct {
	cfg    Config
	up     net.Conn
	ccfg   *vnc.ClientConfig
	scfg   *vnc.ServerConfig
	secure vnc.SecurityHandler
	name   string

	mu      sync.Mutex
	clients map[*downstream]struct{}

	cc          *vnc.ClientConn
	closeSignal chan struct{}
	closed      bool
	closeOnce   sync.Once
}

// downstream is a single attached RFB client.
type downstream struct {
	id       uint64
	conn     net.Conn
	sc       *vnc.ServerConn
	out      chan vnc.ServerMessage
	in       chan vnc.ClientMessage
	encs     map[vnc.EncodingType]bool
	mu       sync.Mutex
	done     chan struct{}
	doneOnce sync.Once
}

var (
	errClosed = errors.New("proxy: closed")
	errNoHost = errors.New("proxy: UpstreamHost (or Upstream) is required")
	errBadCfg = errors.New("proxy: invalid configuration")
)

// New dials (or adopts) the upstream ATEN iKVM connection, performs the
// RFB client handshake and returns a ready Proxy. The proxy starts
// serving downstream clients immediately; use the returned *net.Listener
// from Serve to accept them.
//
// The returned Proxy must eventually be closed via Close; all attached
// downstream clients are disconnected at that point.
func New(cfg Config) (*Proxy, error) {
	if cfg.Upstream == nil {
		if cfg.UpstreamHost == "" {
			return nil, errNoHost
		}
		dial := cfg.Dialer
		if dial == nil {
			dial = func(network, addr string) (net.Conn, error) {
				return net.DialTimeout(network, addr, 10*time.Second)
			}
		}
		up, err := dial("tcp", cfg.UpstreamHost)
		if err != nil {
			return nil, fmt.Errorf("proxy: dial upstream %q: %w", cfg.UpstreamHost, err)
		}
		cfg.Upstream = up
	}

	p := &Proxy{
		cfg:     cfg,
		up:      cfg.Upstream,
		clients: make(map[*downstream]struct{}),
	}

	secure := cfg.DownstreamSecurity
	if secure == nil {
		secure = &vnc.ServerAuthNone{}
	}
	p.secure = secure
	p.name = cfg.DownstreamName
	if p.name == "" {
		p.name = "ATEN iKVM"
	}

	p.ccfg = p.clientConfig()
	cc, err := vnc.NewClientConn(p.up, p.ccfg)
	if err != nil {
		_ = p.up.Close()
		return nil, fmt.Errorf("proxy: client conn: %w", err)
	}
	p.cc = cc
	p.closeSignal = make(chan struct{})

	// Run the RFB client handshake synchronously, then start the
	// blocking message handler in a goroutine. We deliberately do not
	// use vnc.Connect here: Connect also starts the message handler,
	// which never returns, so nothing after the call would ever run.
	if err := p.runClientHandshake(); err != nil {
		_ = p.up.Close()
		return nil, err
	}
	go p.runClientMessages()

	// Watch for upstream failures and close everything on error. Select on
	// closeSignal as well: the read goroutine only pushes to ErrorCh on its
	// own failure path, so if the upstream dies by another means (or the
	// proxy is closed first) this watcher would otherwise block forever on
	// an unread channel and leak.
	go func() {
		select {
		case <-p.ccfg.ErrorCh:
			p.Close()
		case <-p.closeSignal:
		}
	}()

	// Drive the upstream message pump.
	go p.Run()

	// Keep the upstream session (and idle path) alive.
	ka := p.cfg.KeepAliveInterval
	if ka <= 0 {
		ka = 30 * time.Second
	}
	if ka > 0 {
		go p.keepAlive(ka)
	}

	return p, nil
}

// keepAlive periodically sends a no-op incremental framebuffer update
// request upstream. ATEN iKVM answers such requests with a full
// (hermon) framebuffer update, which exercises the whole video path and
// prevents idle sessions from being dropped.
func (p *Proxy) keepAlive(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		select {
		case <-p.closeSignal:
			return
		default:
		}
		p.sendUpstream(&vnc.FramebufferUpdateRequest{
			Inc:    1,
			X:      0,
			Y:      0,
			Width:  800,
			Height: 600,
		})
	}
}

// clientConfig builds the upstream ClientConfig shared by this proxy.
//
// The ATEN private server messages are included so that the upstream
// reader can decode them (and so they can be dropped before reaching
// downstream clients).
func (p *Proxy) clientConfig() *vnc.ClientConfig {
	return &vnc.ClientConfig{
		SecurityHandlers: []vnc.SecurityHandler{
			&vnc.ClientAuthATEN{
				Username: []byte(p.cfg.UpstreamUser),
				Password: []byte(p.cfg.UpstreamPassword),
			},
		},
		Encodings: []vnc.Encoding{
			&vnc.RawEncoding{},
		},
		PixelFormat: vnc.NewPixelFormatAten(),
		Messages: append(
			// Copy DefaultServerMessages first: append writes into the
			// shared package-level slice's backing array once it has spare
			// capacity, corrupting it for every other user of the library.
			append([]vnc.ServerMessage{}, vnc.DefaultServerMessages...),
			&vnc.AteniKVMFrontGroundEvent{},
			&vnc.AteniKVMKeepAliveEvent{},
			&vnc.AteniKVMVideoGetInfo{},
			&vnc.AteniKVMMouseGetInfo{},
			&vnc.AteniKVMSessionMessage{},
			&vnc.AteniKVMGetViewerLang{},
		),
		ClientMessageCh: make(chan vnc.ClientMessage, 256),
		ServerMessageCh: make(chan vnc.ServerMessage, 256),
		ErrorCh:         make(chan error, 1),
	}
}

// runClientHandshake executes the four client handshake handlers in
// order.
func (p *Proxy) runClientHandshake() error {
	handlers := []vnc.Handler{
		&vnc.DefaultClientVersionHandler{},
		&vnc.DefaultClientSecurityHandler{},
		&vnc.DefaultClientClientInitHandler{},
		&vnc.DefaultClientServerInitHandler{},
	}
	for _, h := range handlers {
		if err := h.Handle(p.clientConn()); err != nil {
			return fmt.Errorf("proxy: client handshake %T: %w", h, err)
		}
	}
	return nil
}

// clientConn returns the proxy's upstream *vnc.ClientConn.
func (p *Proxy) clientConn() *vnc.ClientConn {
	return p.cc
}

// runClientMessages runs the blocking upstream message handler.
func (p *Proxy) runClientMessages() {
	if err := (&vnc.DefaultClientMessageHandler{}).Handle(p.clientConn()); err != nil {
		_ = err
	}
}

// Serve accepts downstream connections on ln until the listener or the
// proxy is closed. Each accepted connection is served independently.
func (p *Proxy) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			if p.isClosed() {
				return errClosed
			}
			// A listener error (closed listener, etc.) is permanent, not
			// transient: there is nothing left to accept. Returning keeps a
			// closed-but-not-proxied listener from busy-spinning Accept in a
			// tight loop at 100% CPU. Callers stop this loop via Close, which
			// also surfaces errClosed.
			return err
		}
		go p.serveDownstream(c)
	}
}

// serveDownstream runs the RFB server handshake for one client,
// registers it, and blocks in the message handler until the client
// disconnects or an error occurs.
func (p *Proxy) serveDownstream(c net.Conn) {
	if p.isClosed() {
		_ = c.Close()
		return
	}

	d := &downstream{
		conn: c,
		out:  make(chan vnc.ServerMessage, 256),
		in:   make(chan vnc.ClientMessage, 256),
		encs: make(map[vnc.EncodingType]bool),
		done: make(chan struct{}),
	}
	sc, err := vnc.NewServerConn(c, p.serverConfig(d))
	if err != nil {
		_ = c.Close()
		return
	}
	d.sc = sc

	handlers := []vnc.Handler{
		&vnc.DefaultServerVersionHandler{},
		&vnc.DefaultServerSecurityHandler{},
		&vnc.DefaultServerClientInitHandler{},
		&vnc.DefaultServerServerInitHandler{},
	}
	for _, h := range handlers {
		if err := h.Handle(sc); err != nil {
			_ = c.Close()
			return
		}
	}

	// Register before starting the message handler so frames are
	// already visible to the pump.
	p.addClient(d)
	go p.pumpClientInput(d)
	// Handle blocks until the client disconnects or errors.
	if err := (&vnc.DefaultServerMessageHandler{}).Handle(sc); err != nil {
		_ = err
	}
	p.removeClient(d)
	d.finish()
}

// serverConfig builds the per-client downstream ServerConfig.
func (p *Proxy) serverConfig(d *downstream) *vnc.ServerConfig {
	return &vnc.ServerConfig{
		SecurityHandlers: []vnc.SecurityHandler{p.secure},
		Encodings: []vnc.Encoding{
			&vnc.CopyRectEncoding{},
			&vnc.RawEncoding{},
			&vnc.TightPngEncoding{},
		},
		PixelFormat:     vnc.PixelFormat32bit,
		Width:           800,
		Height:          600,
		DesktopName:     []byte(p.name),
		Messages:        vnc.DefaultClientMessages,
		ClientMessageCh: d.in,
		ServerMessageCh: d.out,
		ErrorCh:         make(chan error, 1),
	}
}

// addClient registers a downstream client with the proxy.
func (p *Proxy) addClient(d *downstream) {
	p.mu.Lock()
	p.clients[d] = struct{}{}
	p.mu.Unlock()
}

// removeClient unregisters a downstream client.
func (p *Proxy) removeClient(d *downstream) {
	p.mu.Lock()
	delete(p.clients, d)
	p.mu.Unlock()
}

// finish marks a downstream client as done.
func (d *downstream) finish() {
	d.doneOnce.Do(func() {
		close(d.done)
	})
}

// isClosed reports whether the proxy has been closed.
func (p *Proxy) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// Close shuts the proxy down: all downstream clients are disconnected
// and the upstream connection is closed. Close is idempotent and safe
// to call concurrently.
func (p *Proxy) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		clients := make([]*downstream, 0, len(p.clients))
		for d := range p.clients {
			clients = append(clients, d)
		}
		p.mu.Unlock()
		close(p.closeSignal)
		for _, d := range clients {
			d.finish()
			_ = d.conn.Close()
		}
		_ = p.up.Close()
	})
}

// pumpClientInput forwards downstream client messages to the proxy's
// input handling. It exits when the client finishes or the proxy closes.
func (p *Proxy) pumpClientInput(d *downstream) {
	for {
		select {
		case <-d.done:
			return
		case <-p.closeCh():
			return
		case msg, ok := <-d.in:
			if !ok {
				return
			}
			p.handleClientMessage(d, msg)
		}
	}
}

// closeCh is the proxy close signal. It is derived from the upstream
// error channel; we also expose it here so input pumps can stop when the
// proxy is torn down.
func (p *Proxy) closeCh() <-chan struct{} {
	return p.closeSignal
}

// handleClientMessage translates a downstream client message into the
// upstream wire protocol.
func (p *Proxy) handleClientMessage(d *downstream, msg vnc.ClientMessage) {
	switch m := msg.(type) {
	case *vnc.SetEncodings:
		// Record the encodings the client wants; the pump picks the
		// right re-encoding per client. The message REPLACES the set
		// (RFB §8.3), so clear before inserting; do NOT forward
		// upstream: the upstream stream is hermon regardless.
		d.mu.Lock()
		d.encs = make(map[vnc.EncodingType]bool)
		for _, e := range m.Encodings {
			d.encs[e] = true
		}
		d.mu.Unlock()
	case *vnc.KeyEvent:
		km := &vnc.AteniKVMKeyEvent{
			Down: m.Down,
			Key:  m.Key,
		}
		p.sendUpstream(km)
	case *vnc.PointerEvent:
		pm := &vnc.AteniKVMPointerEvent{
			Mask: m.Mask,
			X:    m.X,
			Y:    m.Y,
		}
		p.sendUpstream(pm)
	case *vnc.SetPixelFormat:
		// The upstream pixel format is fixed by ATEN (RGB555 16-bit);
		// the downstream pixel format is applied by the transcoder.
		// Ignore.
	case *vnc.FramebufferUpdateRequest:
		// The upstream reader already requests the full screen after
		// every update; downstream incremental requests are dropped.
	case *vnc.ClientCutText:
		// No clipboard bridging to ATEN; drop.
	default:
		// Unknown client messages are dropped.
	}
}

// sendUpstream enqueues a client message to be written to the upstream
// connection. It never blocks.
func (p *Proxy) sendUpstream(msg vnc.ClientMessage) {
	select {
	case p.ccfg.ClientMessageCh <- msg:
	default:
		// Drop if the upstream writer is busy.
	}
}

// Run drives the upstream message pump. It reads server messages off the
// client channel and fans them out to downstream clients. Upstream errors
// are NOT consumed here: the goroutine started by New is the sole reader of
// the client ErrorCh and calls Close on failure, which signals Run to stop.
func (p *Proxy) Run() {
	for {
		select {
		case <-p.closeSignal:
			return
		case msg, ok := <-p.ccfg.ServerMessageCh:
			if !ok {
				return
			}
			p.handleServerMessage(msg)
		}
	}
}

// handleServerMessage processes one upstream server message.
func (p *Proxy) handleServerMessage(msg vnc.ServerMessage) {
	switch m := msg.(type) {
	case *vnc.FramebufferUpdate:
		fu := transcodeFBUpdate(m)
		if fu == nil {
			return
		}
		fanOut(p, fu)
	default:
		// Non-framebuffer upstream messages (ATEN private keep-alive,
		// foreground events, ...) are not meaningful to standard VNC
		// clients and are dropped.
	}
}
