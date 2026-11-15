package rfb

import (
	"bytes"
	"testing"
)

// TestColorRead16BitTrueColorExpansion verifies that true-color channel
// expansion to 8 bits is computed in uint32, so a 16-bit channel maximum
// (RedMax 65535) does not wrap around in uint16 arithmetic.
func TestColorRead16BitTrueColorExpansion(t *testing.T) {
	pf := &PixelFormat{
		BPP: 32, Depth: 32, BigEndian: 1, TrueColor: 1,
		RedMax: 65535, RedShift: 0,
		GreenMax: 255, GreenShift: 16,
		BlueMax: 255, BlueShift: 24,
	}
	// Pixel (big-endian bytes 40 80 FF FF): R=0xFFFF, G=0x80, B=0x40.
	var buf [4]byte
	buf[0], buf[1], buf[2], buf[3] = 0x40, 0x80, 0xFF, 0xFF

	c := &hextileConn{in: bytes.NewBuffer(buf[:]), pf: pf}
	clr := NewColor(pf, &ColorMap{})
	if err := clr.Read(c); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if clr.R != 255 || clr.G != 128 || clr.B != 64 {
		t.Fatalf("color = (%d,%d,%d), want (255,128,64)", clr.R, clr.G, clr.B)
	}
}
