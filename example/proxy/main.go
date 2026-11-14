package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"strings"
	"sync"
	"time"

	vnc "github.com/unistack-org/go-rfb"
)

type Auth struct {
	Username []byte
	Password []byte
}

type Proxy struct {
	cc    vnc.Conn
	conns chan vnc.Conn
	inp   chan vnc.ClientMessage
	out   chan vnc.ServerMessage
}

var (
	cliconns = make(map[string]*Proxy)
	srvconns = make(map[vnc.Conn]string)
	m        sync.Mutex
)

func newConn(hostport string, password []byte) (vnc.Conn, chan vnc.ClientMessage, chan vnc.ServerMessage, chan vnc.Conn, error) {
	fmt.Printf("new conn to %s with %s\n", hostport, password)
	if cc, ok := cliconns[hostport]; ok {
		return cc.cc, cc.inp, cc.out, cc.conns, nil
	}
	c, err := net.DialTimeout("tcp", hostport, 10*time.Second)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cchServer := make(chan vnc.ServerMessage)
	cchClient := make(chan vnc.ClientMessage)
	errorCh := make(chan error)
	ccfg := &vnc.ClientConfig{
		// The upstream is an ATEN iKVM, so authenticate with the ATEN
		// security type (16) rather than standard VNC. This flips the
		// protocol to "aten1", which makes Rectangle.Read decode a raw
		// encoding as an AtenHermon (16x16 sub-rect tiles + raw blocks).
		SecurityHandlers: []vnc.SecurityHandler{&vnc.ClientAuthATEN{Password: password}},
		// ATEN iKVM uses 16-bit little-endian RGB555. Decoding with this
		// format expands the 5-bit channels to 8-bit for the transcoder.
		PixelFormat:     vnc.NewPixelFormatAten(),
		ClientMessageCh: cchClient,
		ServerMessageCh: cchServer,
		Messages:        vnc.DefaultServerMessages,
		Encodings:       []vnc.Encoding{&vnc.RawEncoding{}},
		ErrorCh:         errorCh,
	}
	csrv := make(chan vnc.Conn)
	inp := make(chan vnc.ClientMessage)
	out := make(chan vnc.ServerMessage)
	fmt.Printf("connect to vnc\n")
	cc, err := vnc.Connect(context.Background(), c, ccfg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	fmt.Printf("connected to vnc %#+v\n", cc)
	ds := &vnc.DefaultClientMessageHandler{}
	go ds.Handle(cc)
	go handleIO(cc, inp, out, csrv)

	return cc, inp, out, csrv, nil
}

func handleIO(cli vnc.Conn, inp chan vnc.ClientMessage, out chan vnc.ServerMessage, csrv chan vnc.Conn) {
	fmt.Printf("handle io\n")
	ccfg := cli.Config().(*vnc.ClientConfig)
	defer cli.Close()
	var conns []vnc.Conn
	//var prepared bool

	for {
		select {
		case err := <-ccfg.ErrorCh:
			for _, srv := range conns {
				srv.Close()
			}
			fmt.Printf("err %v\n", err)
			return
		case msg := <-ccfg.ServerMessageCh:
			// ATEN iKVM server messages: transcode hermon FBU into standard
			// raw rectangles, and drop the proprietary ATEN server messages
			// (types 4/22/51/55/57/60) a standard downstream client can't
			// parse.
			switch m := msg.(type) {
			case *vnc.FramebufferUpdate:
				tu := m.TranscodeAtenHermon()
				if tu == nil || tu.NumRect == 0 {
					// Screen is off / nothing to paint.
					continue
				}
				msg = tu
			default:
				if !isStandardServerMessage(m.Type()) {
					continue
				}
			}
			for _, srv := range conns {
				scfg := srv.Config().(*vnc.ServerConfig)
				scfg.ServerMessageCh <- msg
			}
		case msg := <-inp:
			// messages from real clients
			fmt.Printf("3 %#+v\n", msg)
			switch msg.Type() {
			case vnc.SetPixelFormatMsgType:

			case vnc.SetEncodingsMsgType:
				var encTypes []vnc.EncodingType
				encs := []vnc.Encoding{
					//		&vnc.TightPngEncoding{},
					&vnc.CopyRectEncoding{},
					&vnc.RawEncoding{},
				}
				for _, senc := range encs {
					for _, cenc := range msg.(*vnc.SetEncodings).Encodings {
						if cenc == senc.Type() {
							encTypes = append(encTypes, senc.Type())
						}
					}
				}
				ccfg.ClientMessageCh <- &vnc.SetEncodings{Encodings: encTypes}
			default:
				// Translate standard VNC input into ATEN iKVM HID events;
				// the upstream expects the proprietary 18/16-byte key and
				// pointer messages, not standard RFB ones.
				ccfg.ClientMessageCh <- toAtenClientMessage(msg)
			}
		case msg := <-out:
			fmt.Printf("4 %#+v\n", msg)
		case srv := <-csrv:
			conns = append(conns, srv)
		}

	}

}

// isStandardServerMessage reports whether a server message type is part of
// the standard RFB set (FBU, SetColorMapEntries, Bell, ServerCutText). ATEN
// iKVMs also emit proprietary server messages (types 4/22/51/55/57/60) that a
// standard VNC client cannot parse, so the proxy forwards only the standard
// ones downstream.
func isStandardServerMessage(t vnc.ServerMessageType) bool {
	switch t {
	case vnc.FramebufferUpdateMsgType,
		vnc.SetColorMapEntriesMsgType,
		vnc.BellMsgType,
		vnc.ServerCutTextMsgType:
		return true
	}
	return false
}

// toAtenClientMessage translates a standard VNC client message into the
// equivalent ATEN iKVM HID message. The ATEN iKVM expects proprietary key
// (18-byte) and pointer (16-byte) events rather than standard RFB ones, so
// without this translation the keyboard and mouse would not work. Standard
// SetPixelFormat / SetEncodings / FramebufferUpdateRequest / ClientCutText
// messages are passed through unchanged; anything unrecognised is forwarded
// as-is (it will be ignored upstream if not understood).
func toAtenClientMessage(msg vnc.ClientMessage) vnc.ClientMessage {
	switch m := msg.(type) {
	case *vnc.KeyEvent:
		// Write() maps the keysym to an HID usage and drops unmapped keys.
		return &vnc.AteniKVMKeyEvent{Down: m.Down, Key: m.Key}
	case *vnc.PointerEvent:
		return &vnc.AteniKVMPointerEvent{Mask: m.Mask, X: m.X, Y: m.Y}
	default:
		return msg
	}
}

type HijackHandler struct{}

func (*HijackHandler) Handle(c vnc.Conn) error {
	m.Lock()
	defer m.Unlock()
	hostport, ok := srvconns[c]
	if !ok {
		return fmt.Errorf("client connect in server pool not found")
	}
	proxy, ok := cliconns[hostport]
	if !ok {
		return fmt.Errorf("client connect to qemu not found")
	}
	cfg := c.Config().(*vnc.ServerConfig)
	cfg.ClientMessageCh = proxy.inp
	cfg.ServerMessageCh = proxy.out

	proxy.conns <- c
	ds := &vnc.DefaultServerMessageHandler{}
	go ds.Handle(c)
	return nil
}

type AuthVNCHTTP struct {
	c *http.Client
	vnc.ServerAuthVNC
}

func (auth *AuthVNCHTTP) Auth(c vnc.Conn) error {
	auth.ServerAuthVNC.Challenge = []byte("clodo.ruclodo.ru")
	if err := auth.ServerAuthVNC.WriteChallenge(c); err != nil {
		return err
	}
	if err := auth.ServerAuthVNC.ReadChallenge(c); err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	enc := base64.NewEncoder(base64.StdEncoding, buf)
	enc.Write(auth.ServerAuthVNC.Crypted)
	enc.Close()

	v := url.Values{}
	v.Set("hash", buf.String())
	buf.Reset()
	src, _, _ := net.SplitHostPort(c.Conn().RemoteAddr().String())
	v.Set("ip", src)
	res, err := auth.c.PostForm("https://api.ix.clodo.ru/system/vnc", v)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 || res.Body == nil {
		if res.Body != nil {
			io.Copy(buf, res.Body)
		}
		fmt.Printf("failed to get auth data: code %d body %s\n", res.StatusCode, buf.String())
		defer buf.Reset()
		return fmt.Errorf("failed to get auth data: code %d body %s", res.StatusCode, buf.String())
	}
	_, err = io.Copy(buf, res.Body)
	if err != nil {
		return fmt.Errorf("failed to get auth data: %s", err.Error())
	}
	log.Printf("http auth: %s\n", buf.Bytes())
	res.Body.Close()
	data := strings.Split(buf.String(), " ")
	if len(data) < 2 {
		return fmt.Errorf("failed to get auth data data invalid")
	}
	buf.Reset()

	hostport := string(data[0])
	password := []byte(data[1])

	m.Lock()
	defer m.Unlock()
	cc, inp, out, conns, err := newConn(hostport, password)
	if err != nil {
		return err
	}
	cliconns[hostport] = &Proxy{cc, conns, inp, out}
	srvconns[c] = hostport
	c.SetWidth(cc.Width())
	c.SetHeight(cc.Height())
	return nil
}

func (*AuthVNCHTTP) Type() vnc.SecurityType {
	return vnc.SecTypeVNC
}

func (*AuthVNCHTTP) SubType() vnc.SecuritySubType {
	return vnc.SecSubTypeUnknown
}

func main() {
	go func() {
		log.Println(http.ListenAndServe(":6060", nil))
	}()

	ln, err := net.Listen("tcp", ":6900")
	if err != nil {
		log.Fatalf("Error listen. %v", err)
	}

	schClient := make(chan vnc.ClientMessage)
	schServer := make(chan vnc.ServerMessage)

	scfg := &vnc.ServerConfig{
		SecurityHandlers: []vnc.SecurityHandler{
			&AuthVNCHTTP{c: &http.Client{}},
		},
		Encodings: []vnc.Encoding{
			//		&vnc.TightPngEncoding{},
			&vnc.CopyRectEncoding{},
			&vnc.RawEncoding{},
		},
		PixelFormat:     vnc.PixelFormat32bit,
		ClientMessageCh: schClient,
		ServerMessageCh: schServer,
		Messages:        vnc.DefaultClientMessages,
		DesktopName:     []byte("vnc proxy"),
	}
	scfg.Handlers = append(scfg.Handlers, vnc.DefaultServerHandlers...)
	scfg.Handlers = append(scfg.Handlers[:len(scfg.Handlers)-1], &HijackHandler{})
	vnc.Serve(context.Background(), ln, scfg)
}
