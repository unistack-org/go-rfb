package rfb

import (
	"bytes"
	"testing"
)

// writeConn is an in-memory Conn that captures bytes written by Write; it
// reuses hextileConn for the read side.
type writeConn struct {
	hextileConn
	out *bytes.Buffer
}

func (w *writeConn) Write(p []byte) (int, error) { return w.out.Write(p) }

// rectHeader appends a 16-byte rectangle header (x, y, w, h, encType).
func rectHeader(b *bytes.Buffer, x, y, w, h uint16, enc EncodingType) {
	putBE16(b, x)
	putBE16(b, y)
	putBE16(b, w)
	putBE16(b, h)
	putBE32(b, uint32(enc))
}

// readPseudoRect reads one rectangle whose encoding carries no (or a known
// amount of) trailing data, and returns the consumed rectangle.
func readPseudoRect(t *testing.T, b *bytes.Buffer, enc EncodingType) *Rectangle {
	t.Helper()
	c := &hextileConn{in: b, pf: PixelFormat32bit}
	rect := &Rectangle{}
	if err := rect.Read(c); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if rect.EncType != enc {
		t.Fatalf("rect.EncType = %s, want %s", rect.EncType, enc)
	}
	if b.Len() != 0 {
		t.Fatalf("leftover wire bytes after read: %d", b.Len())
	}
	return rect
}

func TestLastRectDispatch(t *testing.T) {
	b := be()
	rectHeader(b, 0, 0, 0, 0, EncLastRectPseudo)
	rect := readPseudoRect(t, b, EncLastRectPseudo)
	if _, ok := rect.Enc.(*LastRectPseudoEncoding); !ok {
		t.Fatalf("rect.Enc = %T, want *LastRectPseudoEncoding", rect.Enc)
	}
}

func TestQEMUPointerMotionDispatch(t *testing.T) {
	b := be()
	rectHeader(b, 1, 0, 0, 0, EncQEMUPointerMotionChangePseudo)
	rect := readPseudoRect(t, b, EncQEMUPointerMotionChangePseudo)
	if _, ok := rect.Enc.(*QEMUPointerMotionChangeEncoding); !ok {
		t.Fatalf("rect.Enc = %T, want *QEMUPointerMotionChangeEncoding", rect.Enc)
	}
	// The x-position (1) is the requested pointer motion mode.
	if rect.X != 1 {
		t.Fatalf("rect.X = %d, want 1", rect.X)
	}
}

func TestQEMUExtendedKeyEventDispatch(t *testing.T) {
	b := be()
	rectHeader(b, 0, 0, 0, 0, EncQEMUExtendedKeyEventPseudo)
	rect := readPseudoRect(t, b, EncQEMUExtendedKeyEventPseudo)
	if _, ok := rect.Enc.(*QEMUExtendedKeyEventEncoding); !ok {
		t.Fatalf("rect.Enc = %T, want *QEMUExtendedKeyEventEncoding", rect.Enc)
	}
}

func TestQEMUAudioDispatch(t *testing.T) {
	b := be()
	rectHeader(b, 0, 0, 0, 0, EncQEMUAudioPseudo)
	rect := readPseudoRect(t, b, EncQEMUAudioPseudo)
	if _, ok := rect.Enc.(*QEMUAudioEncoding); !ok {
		t.Fatalf("rect.Enc = %T, want *QEMUAudioEncoding", rect.Enc)
	}
}

func TestContinuousUpdatesDispatch(t *testing.T) {
	b := be()
	rectHeader(b, 0, 0, 0, 0, EncContinuousUpdatesPseudo)
	rect := readPseudoRect(t, b, EncContinuousUpdatesPseudo)
	if _, ok := rect.Enc.(*ContinuousUpdatesEncoding); !ok {
		t.Fatalf("rect.Enc = %T, want *ContinuousUpdatesEncoding", rect.Enc)
	}
}

func TestExtendedDesktopSizeRead(t *testing.T) {
	b := be()
	// New framebuffer is 1920x1080, reason=0 (non-RFB change), status=0.
	rectHeader(b, 0, 0, 1920, 1080, EncExtendedDesktopSizePseudo)
	// number-of-screens = 2, 3 reserved padding bytes.
	b.Write([]byte{2, 0, 0, 0})
	// Screen 0: id=1, x=0, y=0, w=1920, h=1080, flags=0.
	b.Write([]byte{0, 0, 0, 1}) // id=1
	b.Write([]byte{0, 0})       // x
	b.Write([]byte{0, 0})       // y
	b.Write([]byte{0x07, 0x80}) // w=1920
	b.Write([]byte{0x04, 0x38}) // h=1080
	b.Write([]byte{0, 0, 0, 0}) // flags

	// Screen 1: id=2, x=1920, y=0, w=800, h=600, flags=1.
	b.Write([]byte{0, 0, 0, 2}) // id=2
	b.Write([]byte{0x07, 0x80}) // x=1920
	b.Write([]byte{0, 0})       // y=0
	b.Write([]byte{0x03, 0x20}) // w=800
	b.Write([]byte{0x02, 0x58}) // h=600
	b.Write([]byte{0, 0, 0, 1}) // flags=1

	c := &hextileConn{in: b, pf: PixelFormat32bit}
	rect := &Rectangle{}
	if err := rect.Read(c); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if b.Len() != 0 {
		t.Fatalf("leftover wire bytes after read: %d", b.Len())
	}

	enc, ok := rect.Enc.(*ExtendedDesktopSizeEncoding)
	if !ok {
		t.Fatalf("rect.Enc = %T, want *ExtendedDesktopSizeEncoding", rect.Enc)
	}
	if enc.Reason != 0 || enc.Status != 0 {
		t.Fatalf("Reason/Status = %d/%d, want 0/0", enc.Reason, enc.Status)
	}
	if len(enc.Screens) != 2 {
		t.Fatalf("len(Screens) = %d, want 2", len(enc.Screens))
	}
	s0 := enc.Screens[0]
	if s0.ID != 1 || s0.X != 0 || s0.Y != 0 || s0.Width != 1920 || s0.Height != 1080 || s0.Flags != 0 {
		t.Fatalf("screen0 = %+v", s0)
	}
	s1 := enc.Screens[1]
	if s1.ID != 2 || s1.X != 1920 || s1.Y != 0 || s1.Width != 800 || s1.Height != 600 || s1.Flags != 1 {
		t.Fatalf("screen1 = %+v", s1)
	}
}

func TestExtendedDesktopSizeRoundTrip(t *testing.T) {
	want := &ExtendedDesktopSizeEncoding{
		Reason: 1,
		Status: 0,
		Screens: []ExtendedDesktopSizeScreen{
			{ID: 7, X: 10, Y: 20, Width: 640, Height: 480, Flags: 3},
		},
	}
	out := &bytes.Buffer{}
	wc := &writeConn{hextileConn: hextileConn{pf: PixelFormat32bit}, out: out}
	if err := want.Write(wc, &Rectangle{X: 1, Y: 0, Width: 640, Height: 480, EncType: EncExtendedDesktopSizePseudo}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := &ExtendedDesktopSizeEncoding{}
	// Rebuild the rect header so Read can recover Reason/Status from X/Y.
	c := &hextileConn{in: out, pf: PixelFormat32bit}
	if err := got.Read(c, &Rectangle{X: 1, Y: 0, EncType: EncExtendedDesktopSizePseudo}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Reason != want.Reason || got.Status != want.Status {
		t.Fatalf("Reason/Status = %d/%d, want %d/%d", got.Reason, got.Status, want.Reason, want.Status)
	}
	if len(got.Screens) != 1 {
		t.Fatalf("len(Screens) = %d, want 1", len(got.Screens))
	}
	if got.Screens[0] != want.Screens[0] {
		t.Fatalf("screen = %+v, want %+v", got.Screens[0], want.Screens[0])
	}
	if c.in.Len() != 0 {
		t.Fatalf("leftover wire bytes after read: %d", c.in.Len())
	}
}
