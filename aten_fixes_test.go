package rfb

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// fakeConn is a minimal in-memory Conn backed by an input buffer (for Read)
// and an output buffer (for Write). It implements only the methods the tests
// exercise; the rest return zero values.
type fakeConn struct {
	in   *bytes.Buffer
	out  bytes.Buffer
	w    uint16
	h    uint16
	proto string
}

func (f *fakeConn) Read(p []byte) (int, error)  { return f.in.Read(p) }
func (f *fakeConn) Write(p []byte) (int, error) { return f.out.Write(p) }
func (f *fakeConn) Close() error                { return nil }
func (f *fakeConn) Flush() error                { return nil }
func (f *fakeConn) Conn() net.Conn              { return nil }
func (f *fakeConn) Config() interface{}         { return nil }
func (f *fakeConn) Protocol() string            { return f.proto }
func (f *fakeConn) PixelFormat() *PixelFormat   { return nil }
func (f *fakeConn) SetPixelFormat(*PixelFormat) error { return nil }
func (f *fakeConn) ColorMap() ColorMap          { return ColorMap{} }
func (f *fakeConn) SetColorMap(ColorMap)        {}
func (f *fakeConn) Encodings() []Encoding       { return nil }
func (f *fakeConn) SetEncodings([]EncodingType) error { return nil }
func (f *fakeConn) Width() uint16               { return f.w }
func (f *fakeConn) Height() uint16              { return f.h }
func (f *fakeConn) SetWidth(uint16)             {}
func (f *fakeConn) SetHeight(uint16)            {}
func (f *fakeConn) DesktopName() []byte         { return nil }
func (f *fakeConn) SetDesktopName([]byte)       {}
func (f *fakeConn) Wait()                       {}
func (f *fakeConn) SetProtoVersion(string)      {}
func (f *fakeConn) SetSecurityHandler(SecurityHandler) error { return nil }
func (f *fakeConn) SecurityHandler() SecurityHandler { return nil }

func be(b ...byte) *bytes.Buffer { return bytes.NewBuffer(b) }

// putU16/putU8 append little/big-endian values to a buffer.
func putBE16(b *bytes.Buffer, v uint16) {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	b.Write(buf[:])
}

func putBE32(b *bytes.Buffer, v uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	b.Write(buf[:])
}

// fbuMsg is a FramebufferUpdate that reports the canonical server type (0),
// overriding the promoted Type() so it is usable with readAndRequestNext.
type fbuMsg struct {
	FramebufferUpdate
}

func (f *fbuMsg) Type() ServerMessageType { return FramebufferUpdateMsgType }
func (f *fbuMsg) String() string          { return "FramebufferUpdate" }

// --- PR#1: ATEN server message Read must consume the exact body length ---

func TestAtenServerMessageReadByteAccounting(t *testing.T) {
	cases := []struct {
		typ    ServerMessageType
		body   int // exact number of body bytes on the wire
		expect string
	}{
		{AteniKVMFrontGroundEventMsgType, 20, "AteniKVMFrontGroundEvent"},
		{AteniKVMKeepAliveEventMsgType, 1, "AteniKVMKeepAliveEvent"},
		{AteniKVMVideoGetInfoMsgType, 4, "AteniKVMVideoGetInfo"},
		{AteniKVMMouseGetInfoMsgType, 2, "AteniKVMMouseGetInfo"},
		{AteniKVMSessionMessageMsgType, 264, "AteniKVMSessionMessage"},
		{AteniKVMGetViewerLangMsgType, 8, "AteniKVMGetViewerLang"},
	}

	msgs := []ServerMessage{
		&AteniKVMFrontGroundEvent{},
		&AteniKVMKeepAliveEvent{},
		&AteniKVMVideoGetInfo{},
		&AteniKVMMouseGetInfo{},
		&AteniKVMSessionMessage{},
		&AteniKVMGetViewerLang{},
	}

	for i, tc := range cases {
		t.Run(tc.expect, func(t *testing.T) {
			// Fill a buffer with the body, plus a sentinel byte that must
			// survive a correct Read (proves no over/under-consumption).
			in := be()
			in.Write(make([]byte, tc.body))
			in.WriteByte(0xAA) // sentinel
			fc := &fakeConn{in: in}

			got, err := msgs[i].Read(fc)
			if err != nil {
				t.Fatalf("Read(%v) error: %v", tc.typ, err)
			}
			if got.Type() != tc.typ {
				t.Fatalf("Read type = %v, want %v", got.Type(), tc.typ)
			}
			if got.Supported(fc) != false {
				t.Fatalf("Supported = true, want false for %s", tc.expect)
			}
			if remaining := fc.in.Len(); remaining != 1 {
				t.Fatalf("bytes left after Read = %d, want 1 (only the sentinel); body=%d", remaining, tc.body)
			}
			sentinel, _ := fc.in.ReadByte()
			if sentinel != 0xAA {
				t.Fatalf("sentinel = %#x, want 0xAA (Read consumed wrong length)", sentinel)
			}
		})
	}
}

// --- PR#2: a FramebufferUpdate must trigger an incremental re-request ---

func TestReadAndRequestNextSendsFramebufferRequest(t *testing.T) {
	// Build a FramebufferUpdate wire payload: pad(1) + numRects(2). With
	// zero rectangles no pixel/Hermon data follows, so the read needs no
	// PixelFormat or buffer — mirroring the ATEN "empty frame" case.
	in := be()
	in.WriteByte(0) // pad
	putBE16(in, 0)  // numRects = 0

	fc := &fakeConn{in: in, w: 1024, h: 768}
	fc.SetWidth(1024)
	fc.SetHeight(768)

	parsed, err := readAndRequestNext(fc, &fbuMsg{})
	if err != nil {
		t.Fatalf("readAndRequestNext error: %v", err)
	}
	fbu, ok := parsed.(*FramebufferUpdate)
	if !ok {
		t.Fatalf("parsed type = %T, want *FramebufferUpdate", parsed)
	}
	if fbu.NumRect != 0 || len(fbu.Rects) != 0 {
		t.Fatalf("NumRect=%d rects=%d, want 0/0", fbu.NumRect, len(fbu.Rects))
	}

	// The re-request must be an 11-byte incremental FramebufferUpdateRequest
	// using the connection's dimensions: type(3) inc(1) x(2) y(2) w(2) h(2).
	want := []byte{
		0x03, // FramebufferUpdateRequestMsgType
		0x01, // inc
		0x00, 0x00, // x
		0x00, 0x00, // y
		0x04, 0x00, // width = 1024
		0x03, 0x00, // height = 768
	}
	if !bytes.Equal(fc.out.Bytes(), want) {
		t.Fatalf("re-request wire = % X, want % X", fc.out.Bytes(), want)
	}
}

// A non-framebuffer server message must NOT trigger a re-request.
func TestReadAndRequestNextNonFBU(t *testing.T) {
	in := be()
	in.Write(make([]byte, 4)) // AteniKVMVideoGetInfo body
	fc := &fakeConn{in: in}

	parsed, err := readAndRequestNext(fc, &AteniKVMVideoGetInfo{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if _, ok := parsed.(*AteniKVMVideoGetInfo); !ok {
		t.Fatalf("parsed = %T, want *AteniKVMVideoGetInfo", parsed)
	}
	if fc.out.Len() != 0 {
		t.Fatalf("unexpected re-request bytes written: % X", fc.out.Bytes())
	}
}

// --- PR#3: ATEN keyboard events carry USB-HID usage codes ---

func TestAtenKeyEventWriteHIDTranslation(t *testing.T) {
	fc := &fakeConn{}

	// Return (keysym 0xff0d) -> HID 0x28. Wire: type(4) + 17-byte struct.
	msg := &AteniKVMKeyEvent{Down: 1, Key: Return}
	if err := msg.Write(fc); err != nil {
		t.Fatalf("Write(Return) error: %v", err)
	}
	want := []byte{
		0x04, // key event type
		0x00, // pad
		0x01, // down
		0x00, 0x00, // pad
		0x00, 0x00, 0x00, 0x28, // HID usage 0x28, BE32
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 9 pad
	}
	if !bytes.Equal(fc.out.Bytes(), want) {
		t.Fatalf("key wire = % X, want % X", fc.out.Bytes(), want)
	}
}

func TestAtenKeyEventWriteUnmappedDropped(t *testing.T) {
	fc := &fakeConn{}
	// ModeSwitch has no ATEN mapping; the event must be dropped (nothing written).
	msg := &AteniKVMKeyEvent{Down: 1, Key: ModeSwitch}
	if err := msg.Write(fc); err != nil {
		t.Fatalf("Write(ModeSwitch) error: %v", err)
	}
	if fc.out.Len() != 0 {
		t.Fatalf("unmapped key wrote %d bytes, want 0: % X", fc.out.Len(), fc.out.Bytes())
	}
}

func TestAtenPointerEventWrite(t *testing.T) {
	fc := &fakeConn{}
	msg := &AteniKVMPointerEvent{Mask: 0x01, X: 0x0064, Y: 0x00C8} // 100, 200
	if err := msg.Write(fc); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	want := []byte{
		0x05, // pointer event type
		0x00, // pad
		0x01, // mask
		0x00, 0x64, // x = 100 BE16
		0x00, 0xC8, // y = 200 BE16
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 11 pad
	}
	if !bytes.Equal(fc.out.Bytes(), want) {
		t.Fatalf("pointer wire = % X, want % X", fc.out.Bytes(), want)
	}
}

// A few keysym->HID spot checks (regression guard for the ported table).
func TestKeysymToHID(t *testing.T) {
	cases := []struct {
		key  Key
		hid  uint8
	}{
		{Return, 0x28},
		{A, 0x04},
		{SmallA, 0x04}, // lowercase maps to the same HID code
		{Digit1, 0x1e},
		{Digit0, 0x27},
		{Space, 0x2c},
		{F1, 0x3a},
		{F12, 0x45},
		{Escape, 0x29},
		{ControlLeft, 0xe0},
		{ShiftLeft, 0xe1},
		{AltLeft, 0xe2},
		{SuperLeft, 0xe3},
	}
	for _, tc := range cases {
		got, ok := KeysymToHID(tc.key)
		if !ok {
			t.Fatalf("KeysymToHID(%v) = not ok, want %#x", tc.key, tc.hid)
		}
		if got != tc.hid {
			t.Fatalf("KeysymToHID(%v) = %#x, want %#x", tc.key, got, tc.hid)
		}
	}
	// Unmapped keys must report not-ok.
	if _, ok := KeysymToHID(ModeSwitch); ok {
		t.Fatalf("KeysymToHID(ModeSwitch) should be not ok")
	}
	if _, ok := KeysymToHID(Key(0)); ok {
		t.Fatalf("KeysymToHID(0) should be not ok")
	}
}

// --- PR#4: 15-bit RGB555 channels must be expanded to 8 bits ---

func TestAtenColorReadRGB555Expansion(t *testing.T) {
	pf := NewPixelFormatAten()
	if pf.BPP != 16 || pf.TrueColor != 1 {
		t.Fatalf("unexpected ATEN pixel format: bpp=%d tc=%d", pf.BPP, pf.TrueColor)
	}
	// ATEN pixel format is BigEndian=0 -> order() must be little-endian.
	// Confirm the 16-bit pixels are read little-endian from the stream.
	// Vectors are given as the 16-bit pixel value plus its little-endian wire
	// bytes (ATEN pixel format is BigEndian=0 -> little-endian stream). Each is
	// hand-verified: R5=(px>>10)&31, G5=(px>>5)&31, B5=px&31, then v*255/31.
	cases := []struct {
		name   string
		wire   []byte
		r, g, b uint16
	}{
		// 0x7C00 -> R5=31 -> (255, 0, 0)
		{"pure red", []byte{0x00, 0x7C}, 255, 0, 0},
		// 0x03E0 -> G5=31 -> (0, 255, 0)
		{"pure green", []byte{0xE0, 0x03}, 0, 255, 0},
		// 0x001F -> B5=31 -> (0, 0, 255)
		{"pure blue", []byte{0x1F, 0x00}, 0, 0, 255},
		// 0x7FFF -> all 31 -> (255, 255, 255)
		{"white", []byte{0xFF, 0x7F}, 255, 255, 255},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clr := NewColor(pf, nil)
			if err := clr.Read(&fakeConn{in: be(tc.wire...)}); err != nil {
				t.Fatalf("Read error: %v", err)
			}
			if clr.R != tc.r || clr.G != tc.g || clr.B != tc.b {
				t.Fatalf("got R=%d G=%d B=%d, want R=%d G=%d B=%d", clr.R, clr.G, clr.B, tc.r, tc.g, tc.b)
			}
		})
	}
}

// 8-bit true-color (max=255) must be an identity transform (no expansion).
func TestColorReadIdentityFor255Max(t *testing.T) {
	pf := &PixelFormat{BPP: 8, Depth: 8, BigEndian: 1, TrueColor: 1,
		RedMax: 255, GreenMax: 255, BlueMax: 255, RedShift: 0, GreenShift: 0, BlueShift: 0}
	clr := NewColor(pf, nil)
	in := be(0x00) // R=0,G=0,B=0 packed as 8-bit (no shifts)
	if err := clr.Read(&fakeConn{in: in}); err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if clr.R != 0 || clr.G != 0 || clr.B != 0 {
		t.Fatalf("got R=%d G=%d B=%d, want 0,0,0", clr.R, clr.G, clr.B)
	}
}

// Sanity: the six ATEN server messages and the two client messages all expose
// the documented type bytes (regression guard for the message-type constants).
func TestAtenMessageTypes(t *testing.T) {
	server := map[ServerMessageType]ServerMessage{
		4:  &AteniKVMFrontGroundEvent{},
		22: &AteniKVMKeepAliveEvent{},
		51: &AteniKVMVideoGetInfo{},
		55: &AteniKVMMouseGetInfo{},
		57: &AteniKVMSessionMessage{},
		60: &AteniKVMGetViewerLang{},
	}
	for want, m := range server {
		if m.Type() != want {
			t.Fatalf("%s: Type() = %v, want %v", m, m.Type(), want)
		}
	}
	if (&AteniKVMKeyEvent{}).Type() != AteniKVMKeyEventMsgType {
		t.Fatalf("key event type = %v, want %v", (&AteniKVMKeyEvent{}).Type(), AteniKVMKeyEventMsgType)
	}
	if (&AteniKVMPointerEvent{}).Type() != AteniKVMPointerEventMsgType {
		t.Fatalf("pointer event type = %v, want %v", (&AteniKVMPointerEvent{}).Type(), AteniKVMPointerEventMsgType)
	}
}
