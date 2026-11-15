package proxy

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	vnc "github.com/unistack-org/go-rfb"
)

// be16/be32 are little helpers for building expected wire bytes.
func be16(v uint16) []byte { var b [2]byte; binary.BigEndian.PutUint16(b[:], v); return b[:] }
func be32(v uint32) []byte { var b [4]byte; binary.BigEndian.PutUint32(b[:], v); return b[:] }

// testConn is an in-memory vnc.Conn backed by an input buffer (Read) and an
// output buffer (Write). It is used for the write-then-read round trips in
// the re-encoding unit tests.
type testConn struct {
	in    *bytes.Buffer
	out   *bytes.Buffer
	pf    *vnc.PixelFormat
	width uint16
	h     uint16
}

func (t *testConn) Read(p []byte) (int, error)                   { return t.in.Read(p) }
func (t *testConn) Write(p []byte) (int, error)                  { return t.out.Write(p) }
func (t *testConn) Close() error                                 { return nil }
func (t *testConn) Flush() error                                 { return nil }
func (t *testConn) Conn() net.Conn                               { return nil }
func (t *testConn) Config() interface{}                          { return nil }
func (t *testConn) Protocol() string                             { return "aten1" }
func (t *testConn) PixelFormat() *vnc.PixelFormat                { return t.pf }
func (t *testConn) SetPixelFormat(*vnc.PixelFormat) error        { return nil }
func (t *testConn) ColorMap() vnc.ColorMap                       { return vnc.ColorMap{} }
func (t *testConn) SetColorMap(vnc.ColorMap)                     {}
func (t *testConn) Encodings() []vnc.Encoding                    { return nil }
func (t *testConn) SetEncodings([]vnc.EncodingType) error        { return nil }
func (t *testConn) Width() uint16                                { return t.width }
func (t *testConn) Height() uint16                               { return t.h }
func (t *testConn) SetWidth(uint16)                              {}
func (t *testConn) SetHeight(uint16)                             {}
func (t *testConn) DesktopName() []byte                          { return nil }
func (t *testConn) SetDesktopName([]byte)                        {}
func (t *testConn) Wait()                                        {}
func (t *testConn) SetProtoVersion(string)                       {}
func (t *testConn) SetSecurityHandler(vnc.SecurityHandler) error { return nil }
func (t *testConn) SecurityHandler() vnc.SecurityHandler         { return nil }

// newAtenSubrectHermon builds a hermon FramebufferUpdate carrying a single
// 16x16 sub-rectangle of solid red (RGB555 0x7C00, little-endian) at grid
// position (gx, gy).
func newAtenSubrectHermon(gx, gy uint8) *vnc.FramebufferUpdate {
	const tile = 16
	data := make([]byte, tile*tile*2)
	for i := range data {
		if i%2 == 0 {
			data[i] = 0x00 // low byte
		} else {
			data[i] = 0x7C // high byte
		}
	}
	sr := &vnc.AtenHermonSubrect{A: 0, B: 0, Y: gy, X: gx, Data: data}
	hermon := &vnc.AtenHermon{
		AtenLength:    528,
		AtenType:      0,
		AtenSubrects:  1,
		AtenRawLength: 528,
		Encodings:     []vnc.Encoding{sr},
	}
	return &vnc.FramebufferUpdate{
		NumRect: 1,
		Rects: []*vnc.Rectangle{{
			X: 0, Y: 0, Width: 800, Height: 600,
			EncType: vnc.EncAtenHermon,
			Enc:     hermon,
		}},
	}
}

// TestTranscodeSubrectExpands verifies that a single hermon sub-rectangle is
// expanded into a 16x16 raw rectangle at (gx*16, gy*16) with the RGB555
// pixels decoded to 8-bit R/G/B (0x7C00 -> (255,0,0)).
func TestTranscodeSubrectExpands(t *testing.T) {
	fu := newAtenSubrectHermon(1, 2)
	out := transcodeFBUpdate(fu)
	if out == nil {
		t.Fatal("transcodeFBUpdate returned nil for a hermon with a subrect")
	}
	if len(out.Rects) != 1 {
		t.Fatalf("got %d rects, want 1", len(out.Rects))
	}
	r := out.Rects[0]
	if r.X != 16 || r.Y != 32 || r.Width != 16 || r.Height != 16 {
		t.Fatalf("rect = (%d,%d %dx%d), want (16,32 16x16)", r.X, r.Y, r.Width, r.Height)
	}
	raw, ok := r.Enc.(*vnc.RawEncoding)
	if !ok {
		t.Fatalf("enc = %T, want *RawEncoding", r.Enc)
	}
	if len(raw.Colors) != 256 {
		t.Fatalf("colors len = %d, want 256", len(raw.Colors))
	}
	if raw.Colors[0].R != 255 || raw.Colors[0].G != 0 || raw.Colors[0].B != 0 {
		t.Fatalf("color[0] = (%d,%d,%d), want (255,0,0)", raw.Colors[0].R, raw.Colors[0].G, raw.Colors[0].B)
	}
	if raw.Colors[255].R != 255 || raw.Colors[255].G != 0 || raw.Colors[255].B != 0 {
		t.Fatalf("color[255] = (%d,%d,%d), want (255,0,0)", raw.Colors[255].R, raw.Colors[255].G, raw.Colors[255].B)
	}
}

// TestTranscodeDropsScreenOff verifies that a hermon with no paintable
// children (the ATEN "screen off" sentinel) yields nil.
func TestTranscodeDropsScreenOff(t *testing.T) {
	fu := &vnc.FramebufferUpdate{
		NumRect: 1,
		Rects: []*vnc.Rectangle{{
			X: 0, Y: 0, Width: 800, Height: 600,
			EncType: vnc.EncAtenHermon,
			Enc:     &vnc.AtenHermon{AtenLength: 10, AtenRawLength: 10},
		}},
	}
	if out := transcodeFBUpdate(fu); out != nil {
		t.Fatalf("transcodeFBUpdate = %+v, want nil for screen-off sentinel", out)
	}
}

// TestReencodeSolidFillRoundTrip verifies that a solid raw rectangle is
// re-encoded as a tight Fill rectangle and that a read-back recovers the
// original color.
func TestReencodeSolidFillRoundTrip(t *testing.T) {
	fu := newAtenSubrectHermon(0, 0)
	raw := transcodeFBUpdate(fu)
	rec := reencodeTightPng(raw)
	if len(rec.Rects) != 1 {
		t.Fatalf("reencode produced %d rects, want 1", len(rec.Rects))
	}
	if rec.Rects[0].EncType != vnc.EncTightPng {
		t.Fatalf("encType = %v, want EncTightPng", rec.Rects[0].EncType)
	}

	// Write the re-encoded FBU, then read it back and check the Fill color.
	tc := &testConn{in: bytes.NewBuffer(nil), out: &bytes.Buffer{}, pf: vnc.PixelFormat32bit, width: 800, h: 600}
	if err := rec.Write(tc); err != nil {
		t.Fatalf("write re-encoded FBU: %v", err)
	}
	// FramebufferUpdate.Write emits [type pad numRect]; Read only consumes
	// [pad numRect], so skip the leading type byte before reading.
	tc.in = bytes.NewBuffer(tc.out.Bytes()[1:])

	sm, err := (&vnc.FramebufferUpdate{}).Read(tc)
	if err != nil {
		t.Fatalf("read re-encoded FBU: %v", err)
	}
	read := sm.(*vnc.FramebufferUpdate)
	if len(read.Rects) != 1 {
		t.Fatalf("read %d rects, want 1", len(read.Rects))
	}
	enc, ok := read.Rects[0].Enc.(*vnc.TightPngEncoding)
	if !ok {
		t.Fatalf("read enc = %T, want *TightPngEncoding", read.Rects[0].Enc)
	}
	if enc.TightCC.Compression != vnc.TightCompressionFill {
		t.Fatalf("compression = %v, want Fill", enc.TightCC.Compression)
	}
	r, g, b, _ := enc.Image.At(0, 0).RGBA()
	if int(r>>8) != 255 || int(g>>8) != 0 || int(b>>8) != 0 {
		t.Fatalf("fill color = (%d,%d,%d), want (255,0,0)", r>>8, g>>8, b>>8)
	}
}

// TestReencodeNonSolidPngPath verifies that a non-solid raw rectangle is
// re-encoded as a tight PNG rectangle and that a read-back recovers the image
// dimensions and corner colors.
func TestReencodeNonSolidPngPath(t *testing.T) {
	const tile = 16
	red := vnc.Color{}
	red.R = 255
	green := vnc.Color{}
	green.G = 255
	colors := make([]vnc.Color, tile*tile)
	for y := 0; y < tile; y++ {
		for x := 0; x < tile; x++ {
			if (x+y)%2 == 0 {
				colors[y*tile+x] = red
			} else {
				colors[y*tile+x] = green
			}
		}
	}
	fu := &vnc.FramebufferUpdate{
		NumRect: 1,
		Rects: []*vnc.Rectangle{{
			X: 0, Y: 0, Width: tile, Height: tile,
			EncType: vnc.EncRaw,
			Enc:     &vnc.RawEncoding{Colors: colors},
		}},
	}
	rec := reencodeTightPng(fu)
	enc, ok := rec.Rects[0].Enc.(*vnc.TightPngEncoding)
	if !ok {
		t.Fatalf("enc = %T, want *TightPngEncoding", rec.Rects[0].Enc)
	}
	if enc.TightCC.Compression != vnc.TightCompressionPNG {
		t.Fatalf("compression = %v, want PNG (checkerboard is not solid)", enc.TightCC.Compression)
	}

	// Round-trip through the wire.
	tc := &testConn{in: bytes.NewBuffer(nil), out: &bytes.Buffer{}, pf: vnc.PixelFormat32bit, width: 800, h: 600}
	if err := rec.Write(tc); err != nil {
		t.Fatalf("write re-encoded FBU: %v", err)
	}
	// FramebufferUpdate.Write emits [type pad numRect]; Read only consumes
	// [pad numRect], so skip the leading type byte before reading.
	tc.in = bytes.NewBuffer(tc.out.Bytes()[1:])
	sm, err := (&vnc.FramebufferUpdate{}).Read(tc)
	if err != nil {
		t.Fatalf("read re-encoded FBU: %v", err)
	}
	read := sm.(*vnc.FramebufferUpdate)
	img := read.Rects[0].Enc.(*vnc.TightPngEncoding).Image
	if img.Bounds().Dx() != tile || img.Bounds().Dy() != tile {
		t.Fatalf("decoded bounds = %v, want %dx%d", img.Bounds(), tile, tile)
	}
	if cr, cg, cb, _ := img.At(0, 0).RGBA(); int(cr>>8) != 255 || int(cg>>8) != 0 || int(cb>>8) != 0 {
		t.Fatalf("corner (0,0) = (%d,%d,%d), want red", cr>>8, cg>>8, cb>>8)
	}
	// (15,15): 15+15=30 is even, so the same color as (0,0) — red.
	if cr, cg, cb, _ := img.At(tile-1, tile-1).RGBA(); int(cr>>8) != 255 || int(cg>>8) != 0 || int(cb>>8) != 0 {
		t.Fatalf("corner (%d,%d) = (%d,%d,%d), want red", tile-1, tile-1, cr>>8, cg>>8, cb>>8)
	}
}

// hermonFrame builds the wire bytes of an ATEN iKVM frame-buffer update: one
// hermon rectangle (800x600) containing a single 16x16 solid-red sub-rect at
// the origin.
func hermonFrame() []byte {
	var b bytes.Buffer
	b.Write([]byte{0x00, 0x00}) // FBU type 0 + pad
	b.Write(be16(1))            // NumRect
	// Rect header
	b.Write(be16(0)) // X
	b.Write(be16(0)) // Y
	b.Write(be16(800))
	b.Write(be16(600))
	b.Write(be32(uint32(vnc.EncAtenHermon)))
	// Hermon body
	b.Write(make([]byte, 4)) // pad
	b.Write(be32(528))       // aten_length
	b.WriteByte(0)           // aten_type (subrect)
	b.WriteByte(0)           // pad
	b.Write(be32(1))         // subrects
	b.Write(be32(528))       // raw_length
	// Subrect: A B Y X + 512B (16x16 RGB555 LE, all red = 0x7C00)
	b.Write(be16(0)) // A
	b.Write(be16(0)) // B
	b.WriteByte(0)   // Y
	b.WriteByte(0)   // X
	for i := 0; i < 16*16; i++ {
		b.WriteByte(0x00) // low byte
		b.WriteByte(0x7C) // high byte
	}
	return b.Bytes()
}

// fakeAtenServer runs a minimal ATEN iKVM RFB server on ln: it performs the
// client handshake, then responds to every framebuffer update request with a
// hermon frame. It signals that a key event was received via keyCh.
func fakeAtenServer(t *testing.T, ln net.Listener, keyCh chan<- struct{}) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	rd := func(n int) ([]byte, bool) {
		b := make([]byte, n)
		total := 0
		for total < n {
			m, e := conn.Read(b[total:])
			total += m
			if e != nil {
				return b[:total], false
			}
		}
		return b, true
	}
	wr := func(b []byte) {
		if _, e := conn.Write(b); e != nil {
			return
		}
	}

	// 1. Version
	wr([]byte("RFB 003.008\n"))
	if _, ok := rd(12); !ok {
		return
	}
	// 2. Security: ATEN only
	wr([]byte{0x01, 0x10})
	if _, ok := rd(1); !ok {
		return
	}
	// 3. Tunnel count 0 + 20 skip bytes (24 total); client replies 48B challenge.
	wr(append(be32(0), make([]byte, 20)...))
	if _, ok := rd(48); !ok {
		return
	}
	// 4. authCode 0 (OK); client replies shared flag.
	wr(be32(0))
	if _, ok := rd(1); !ok {
		return
	}
	// 5. ServerInit: 800x600, 16-bit RGB555, name "ATEN iKVM".
	pf := []byte{16, 15, 0, 1, 0x00, 0x1f, 0x00, 0x1f, 0x00, 0x1f, 10, 5, 0, 0, 0, 0}
	name := []byte("ATEN iKVM")
	si := []byte{}
	si = append(si, be16(800)...)
	si = append(si, be16(600)...)
	si = append(si, pf...)
	si = append(si, be32(uint32(len(name)))...)
	si = append(si, name...)
	wr(si)
	// 6. iKVM block (12 bytes).
	wr(make([]byte, 12))
	// 7. Read the client-init block (SetPixelFormat 20 + SetEncodings 8 +
	//    FramebufferUpdateRequest 10 = 38 bytes) in one go so the stream is
	//    aligned before we start emitting frames. Its last 10 bytes are the
	//    initial FBURequest, so answer it with a frame right away; the
	//    proxy's readAndRequestNext then pings further 0x03 requests.
	if _, ok := rd(38); !ok {
		return
	}

	frame := hermonFrame()
	wr(frame) // answer the initial FBURequest carried in the client-init block
	for {
		typ, ok := rd(1)
		if !ok {
			return
		}
		switch typ[0] {
		case 0x03: // FramebufferUpdateRequest (10B)
			if _, ok := rd(9); !ok {
				return
			}
			wr(frame) // reply with a hermon frame
		case 0x04: // ATEN key event (18B total)
			if b, ok := rd(17); !ok {
				return
			} else if len(b) == 17 {
				select {
				case keyCh <- struct{}{}:
				default:
				}
			}
		case 0x05: // ATEN pointer event (18B total)
			if _, ok := rd(17); !ok {
				return
			}
		default:
			// Unknown message: consume a best-effort body and move on.
			if _, ok := rd(4); !ok {
				return
			}
		}
	}
}

// TestProxyEndToEnd wires the proxy between a fake ATEN iKVM upstream and a
// plain RFB 3.8 downstream client. It verifies that:
//   - the upstream hermon frame is transcoded to raw and re-encoded as a
//     tight Fill for the (tightpng-advertising) downstream client, and
//   - a downstream key press is translated to an ATEN iKVM key event
//     (HID) upstream.
func TestProxyEndToEnd(t *testing.T) {
	up, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()

	dn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer dn.Close()

	keyCh := make(chan struct{}, 1)
	go fakeAtenServer(t, up, keyCh)

	px, err := New(Config{
		UpstreamHost:     up.Addr().String(),
		UpstreamUser:     "admin",
		UpstreamPassword: "pass",
	})
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}
	defer px.Close()

	go px.Serve(dn)

	// Connect the downstream RFB client.
	client, err := net.Dial("tcp", dn.Addr().String())
	if err != nil {
		t.Fatalf("dial downstream: %v", err)
	}
	defer client.Close()
	client.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Client-side RFB handshake.
	rd := func(n int) []byte {
		b := make([]byte, n)
		total := 0
		for total < n {
			m, e := client.Read(b[total:])
			total += m
			if e != nil {
				t.Fatalf("client read %d: %v", n, e)
			}
		}
		return b
	}
	if v := rd(12); string(v) != "RFB 003.008\n" {
		t.Fatalf("server version = %q, want RFB 003.008", v)
	}
	client.Write([]byte("RFB 003.008\n"))

	secType := rd(2)
	if secType[0] != 1 || secType[1] != 1 {
		t.Fatalf("security = % x, want [01 01]", secType)
	}
	client.Write([]byte{0x01})

	if ac := rd(4); ac[0] != 0 || ac[1] != 0 || ac[2] != 0 || ac[3] != 0 {
		t.Fatalf("authCode = % x, want 0", ac)
	}
	client.Write([]byte{0x01}) // shared

	// ServerInit: width, height, 16-byte pixel format, nameLen, name.
	// Layout: [0:2]=w, [2:4]=h, [4:20]=PF, [20:24]=nameLen, [24:]=name.
	si := rd(2 + 2 + 16 + 4)
	w := binary.BigEndian.Uint16(si[0:2])
	h := binary.BigEndian.Uint16(si[2:4])
	nameLen := binary.BigEndian.Uint32(si[20:24])
	if w != 800 || h != 600 {
		t.Fatalf("ServerInit = %dx%d, want 800x600", w, h)
	}
	name := rd(int(nameLen))
	if string(name) != "ATEN iKVM" {
		t.Fatalf("desktop name = %q, want ATEN iKVM", name)
	}

	// Advertise raw + tightpng, set the pixel format, request an update.
	enc := []byte{0x02, 0x00, 0x00, 0x02, // SetEncodings, 2 encodings
		0x00, 0x00, 0x00, 0x00} // raw
	encTp := int32(vnc.EncTightPng)
	enc = append(enc, be32(uint32(encTp))...) // tightpng
	client.Write(enc)
	client.Write(append([]byte{0x00, 0x00, 0x00, 0x00},
		[]byte{32, 24, 0, 1, 0xff, 0xff, 0x00, 0xff, 0x00, 0xff, 0x16, 0x08, 0, 0, 0, 0}...))
	client.Write([]byte{0x03, 0x00, 0, 0, 0, 0, 800 & 0xff, 800 >> 8, 600 & 0xff, 600 >> 8})

	foundTightPng := false
	for i := 0; i < 16; i++ {
		fbuHdr := rd(4)
		if fbuHdr[0] != 0x00 {
			t.Fatalf("FBU type = %#x, want 0", fbuHdr[0])
		}
		numRect := binary.BigEndian.Uint16(fbuHdr[2:4])
		if numRect != 1 {
			t.Fatalf("FBU numRect = %d, want 1", numRect)
		}
		// Rect: X Y W H (8) + EncType (4) = 12 bytes.
		rh := rd(12)
		encType := int32(binary.BigEndian.Uint32(rh[8:12]))
		if encType == int32(vnc.EncTightPng) {
			// Fill payload: CC (0x80) + R G B.
			fill := rd(4)
			if fill[0] != 0x80 {
				t.Fatalf("Fill CC = %#x, want 0x80", fill[0])
			}
			if fill[1] != 0xff || fill[2] != 0x00 || fill[3] != 0x00 {
				t.Fatalf("Fill color = % x, want ff 00 00", fill[1:])
			}
			foundTightPng = true
			break
		}
		// A raw frame was fanned out before the client's SetEncodings was
		// recorded — drain it (16x16 raw = 768 pixels x 4B) and keep going.
		if encType != int32(vnc.EncRaw) {
			t.Fatalf("rect encType = %d, want %d (EncTightPng) or %d (EncRaw)", encType, int32(vnc.EncTightPng), int32(vnc.EncRaw))
		}
		if _, err := client.Read(make([]byte, 16*16*4)); err != nil {
			t.Fatalf("draining raw frame: %v", err)
		}
	}
	if !foundTightPng {
		t.Fatal("no tightpng FBU received within 16 frames")
	}

	// Send a key press ('a', keysym 0x61) downstream.
	client.Write([]byte{0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x61})

	select {
	case <-keyCh:
		// Upstream received the translated ATEN key event.
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not receive the ATEN key event")
	}
}
