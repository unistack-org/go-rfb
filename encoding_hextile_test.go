package rfb

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// hextileConn is an in-memory Conn with a real (truecolor 32-bit) pixel format
// so Color.Read can decode raw/bg/fg pixels. It backs hextile wire bytes in
// f.in and captures nothing on Write (hextile is read-only).
type hextileConn struct {
	in *bytes.Buffer
	pf *PixelFormat
}

func (h *hextileConn) Read(p []byte) (int, error)               { return h.in.Read(p) }
func (h *hextileConn) Write(p []byte) (int, error)              { return len(p), nil }
func (h *hextileConn) Close() error                             { return nil }
func (h *hextileConn) Flush() error                             { return nil }
func (h *hextileConn) Conn() net.Conn                           { return nil }
func (h *hextileConn) Config() interface{}                      { return nil }
func (h *hextileConn) Protocol() string                         { return "" }
func (h *hextileConn) PixelFormat() *PixelFormat                { return h.pf }
func (h *hextileConn) SetPixelFormat(*PixelFormat) error        { return nil }
func (h *hextileConn) ColorMap() ColorMap                       { return ColorMap{} }
func (h *hextileConn) SetColorMap(ColorMap)                     {}
func (h *hextileConn) Encodings() []Encoding                    { return nil }
func (h *hextileConn) SetEncodings([]EncodingType) error        { return nil }
func (h *hextileConn) Width() uint16                            { return 0 }
func (h *hextileConn) Height() uint16                           { return 0 }
func (h *hextileConn) SetWidth(uint16)                          {}
func (h *hextileConn) SetHeight(uint16)                         {}
func (h *hextileConn) DesktopName() []byte                      { return nil }
func (h *hextileConn) SetDesktopName([]byte)                    {}
func (h *hextileConn) Wait()                                    {}
func (h *hextileConn) SetProtoVersion(string)                   {}
func (h *hextileConn) SetSecurityHandler(SecurityHandler) error { return nil }
func (h *hextileConn) SecurityHandler() SecurityHandler         { return nil }

// hp appends a 32-bit truecolor pixel (little-endian, R<<16|G<<8|B) to b.
func hp(b *bytes.Buffer, r, g, bl byte) {
	var buf [4]byte
	px := uint32(r)<<16 | uint32(g)<<8 | uint32(bl)
	binary.LittleEndian.PutUint32(buf[:], px)
	b.Write(buf[:])
}

// hue returns R,G,B for a decoded color (truecolor, 0-255 channels).
func hue(c Color) (r, g, b byte) {
	return byte(c.R), byte(c.G), byte(c.B)
}

func hextileWire(b *bytes.Buffer) *hextileConn {
	return &hextileConn{in: b, pf: PixelFormat32bit}
}

func newHextileRect(w, h int) *Rectangle {
	return &Rectangle{Width: uint16(w), Height: uint16(h), EncType: EncHextile}
}

// TestHextileBackgroundFill: a single 16x16 tile with only the background
// specified fills the whole tile with that color.
func TestHextileBackgroundFill(t *testing.T) {
	b := be()
	b.WriteByte(0x02) // background specified
	hp(b, 10, 20, 30) // bg

	c := hextileWire(b)
	enc := &HextileEncoding{}
	if err := enc.Read(c, newHextileRect(16, 16)); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(enc.Colors) != 256 {
		t.Fatalf("len(Colors) = %d, want 256", len(enc.Colors))
	}
	for i, col := range enc.Colors {
		if r, g, bl := hue(col); r != 10 || g != 20 || bl != 30 {
			t.Fatalf("pixel %d = (%d,%d,%d), want (10,20,30)", i, r, g, bl)
		}
	}
}

// TestHextileRaw: a small 2x2 tile using the RAW sub-encoding.
func TestHextileRaw(t *testing.T) {
	b := be()
	b.WriteByte(0x01) // raw
	hp(b, 1, 1, 1)
	hp(b, 2, 2, 2)
	hp(b, 3, 3, 3)
	hp(b, 4, 4, 4)

	c := hextileWire(b)
	enc := &HextileEncoding{}
	if err := enc.Read(c, newHextileRect(2, 2)); err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := [][3]byte{{1, 1, 1}, {2, 2, 2}, {3, 3, 3}, {4, 4, 4}}
	for i, col := range enc.Colors {
		r, g, bl := hue(col)
		if [3]byte{r, g, bl} != want[i] {
			t.Fatalf("pixel %d = (%d,%d,%d), want %v", i, r, g, bl, want[i])
		}
	}
}

// TestHextileColoredSubrect: bg fill plus one colored sub-rect over it.
func TestHextileColoredSubrect(t *testing.T) {
	b := be()
	// bg + fg + any-subrects + subrects-colored
	b.WriteByte(0x02 | 0x04 | 0x08 | 0x10)
	hp(b, 10, 20, 30)   // bg
	hp(b, 200, 100, 50) // fg (unused by the colored sub-rect, but must be read)
	b.WriteByte(1)      // 1 sub-rect
	hp(b, 1, 2, 3)      // sub-rect color (colored)
	b.WriteByte(0x42)   // x=4 (high nibble), y=2 (low nibble)
	b.WriteByte(0x35)   // w=3 (high nibble), h=5 (low nibble)
	// => sub-rect spans x in [4,7], y in [2,7] (w=4, h=6 after +1).

	c := hextileWire(b)
	enc := &HextileEncoding{}
	if err := enc.Read(c, newHextileRect(16, 16)); err != nil {
		t.Fatalf("Read: %v", err)
	}

	// Inside the sub-rect.
	if r, g, bl := hue(enc.Colors[2*16+4]); r != 1 || g != 2 || bl != 3 {
		t.Fatalf("sub-rect pixel (4,2) = (%d,%d,%d), want (1,2,3)", r, g, bl)
	}
	// Corner of the tile, outside the sub-rect -> bg.
	if r, g, bl := hue(enc.Colors[0]); r != 10 || g != 20 || bl != 30 {
		t.Fatalf("bg pixel (0,0) = (%d,%d,%d), want (10,20,30)", r, g, bl)
	}
	// Just outside the sub-rect to the right (x=8) -> bg.
	if r, g, bl := hue(enc.Colors[2*16+8]); r != 10 || g != 20 || bl != 30 {
		t.Fatalf("bg pixel (8,2) = (%d,%d,%d), want (10,20,30)", r, g, bl)
	}
}

// TestHextileFgSubrect: sub-rects that inherit the foreground color
// (subrects-colored bit NOT set).
func TestHextileFgSubrect(t *testing.T) {
	b := be()
	// bg + fg + any-subrects (not colored)
	b.WriteByte(0x02 | 0x04 | 0x08)
	hp(b, 1, 2, 3)    // bg
	hp(b, 9, 9, 9)    // fg
	b.WriteByte(1)    // 1 sub-rect
	b.WriteByte(0x00) // x=0, y=0
	b.WriteByte(0xFF) // w=16, h=16 -> whole tile

	c := hextileWire(b)
	enc := &HextileEncoding{}
	if err := enc.Read(c, newHextileRect(16, 16)); err != nil {
		t.Fatalf("Read: %v", err)
	}
	for i, col := range enc.Colors {
		if r, g, bl := hue(col); r != 9 || g != 9 || bl != 9 {
			t.Fatalf("pixel %d = (%d,%d,%d), want (9,9,9)", i, r, g, bl)
		}
	}
}

// TestHextileBackgroundCarriesAcrossTiles: a bg declared in one tile is
// inherited by the following tile that omits the bg bit.
func TestHextileBackgroundCarriesAcrossTiles(t *testing.T) {
	b := be()
	// Tile (0,0): bg specified.
	b.WriteByte(0x02)
	hp(b, 10, 20, 30)
	// Tile (16,0): no bits -> inherits bg from the previous tile.
	b.WriteByte(0x00)

	c := hextileWire(b)
	enc := &HextileEncoding{}
	if err := enc.Read(c, newHextileRect(32, 16)); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(enc.Colors) != 32*16 {
		t.Fatalf("len(Colors) = %d, want %d", len(enc.Colors), 32*16)
	}
	for i, col := range enc.Colors {
		if r, g, bl := hue(col); r != 10 || g != 20 || bl != 30 {
			t.Fatalf("pixel %d = (%d,%d,%d), want (10,20,30)", i, r, g, bl)
		}
	}
}

// TestHextileDispatchedFromRectangle: the Rectangle.Read dispatch must select
// HextileEncoding for EncHextile.
func TestHextileDispatchedFromRectangle(t *testing.T) {
	b := be()
	// Rectangle header: x, y, w, h, encType(4 bytes).
	putBE16(b, 0)  // x
	putBE16(b, 0)  // y
	putBE16(b, 16) // w
	putBE16(b, 16) // h
	var encBuf [4]byte
	binary.BigEndian.PutUint32(encBuf[:], uint32(EncHextile))
	b.Write(encBuf[:])
	// Tile body: bg fill.
	b.WriteByte(0x02)
	hp(b, 7, 7, 7)

	c := hextileWire(b)
	rect := NewRectangle()
	if err := rect.Read(c); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if rect.EncType != EncHextile {
		t.Fatalf("rect.EncType = %v, want %v", rect.EncType, EncHextile)
	}
	hex, ok := rect.Enc.(*HextileEncoding)
	if !ok {
		t.Fatalf("rect.Enc = %T, want *HextileEncoding", rect.Enc)
	}
	if r, g, bl := hue(hex.Colors[0]); r != 7 || g != 7 || bl != 7 {
		t.Fatalf("pixel (0,0) = (%d,%d,%d), want (7,7,7)", r, g, bl)
	}
}
