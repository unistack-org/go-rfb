package rfb

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// newRREConn wraps wire bytes b in a truecolor 32-bit conn.
func newRREConn(b *bytes.Buffer) *hextileConn {
	return &hextileConn{in: b, pf: PixelFormat32bit}
}

// TestRREBackgroundOnly: zero sub-rectangles fills the whole rect with the
// background color.
func TestRREBackgroundOnly(t *testing.T) {
	b := be()
	b.Write([]byte{0, 0, 0, 0}) // numSubRects = 0
	hp(b, 10, 20, 30)           // background

	c := newRREConn(b)
	enc := &RREEncoding{}
	if err := enc.Read(c, &Rectangle{Width: 4, Height: 3, EncType: EncRRE}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(enc.Colors) != 12 {
		t.Fatalf("len(Colors) = %d, want 12", len(enc.Colors))
	}
	for i, col := range enc.Colors {
		if r, g, bl := hue(col); r != 10 || g != 20 || bl != 30 {
			t.Fatalf("pixel %d = (%d,%d,%d), want (10,20,30)", i, r, g, bl)
		}
	}
}

// TestRRESubrects: a 16x16 rect with two sub-rectangles over a background.
// Sub-rect 1: x=4 y=2 w=4 h=6 -> (1,2,3); sub-rect 2: x=1 y=1 w=2 h=2 -> (9,8,7).
func TestRRESubrects(t *testing.T) {
	b := be()
	b.Write([]byte{0, 0, 0, 2}) // numSubRects = 2
	hp(b, 0, 0, 0)              // background

	// Sub-rect 1.
	hp(b, 1, 2, 3)
	be16 := func(v uint16) { b.Write([]byte{byte(v >> 8), byte(v)}) }
	be16(4) // x
	be16(2) // y
	be16(4) // w
	be16(6) // h

	// Sub-rect 2 (overlaps sub-rect 1 at (4,2); written later, so wins).
	hp(b, 9, 8, 7)
	be16(1)
	be16(1)
	be16(2)
	be16(2)

	c := newRREConn(b)
	enc := &RREEncoding{}
	if err := enc.Read(c, &Rectangle{Width: 16, Height: 16, EncType: EncRRE}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(enc.Colors) != 256 {
		t.Fatalf("len(Colors) = %d, want 256", len(enc.Colors))
	}

	at := func(x, y int) (r, g, bl byte) {
		return hue(enc.Colors[y*16+x])
	}

	// Outside both sub-rects -> background.
	if r, g, bl := at(0, 0); r != 0 || g != 0 || bl != 0 {
		t.Fatalf("at(0,0) = (%d,%d,%d), want (0,0,0)", r, g, bl)
	}
	// Inside sub-rect 1 only.
	if r, g, bl := at(4, 3); r != 1 || g != 2 || bl != 3 {
		t.Fatalf("at(4,3) = (%d,%d,%d), want (1,2,3)", r, g, bl)
	}
	// Inside sub-rect 2 (overlaps rect 1 region? 2 covers x1..2,y1..2; x4 is outside it).
	if r, g, bl := at(1, 1); r != 9 || g != 8 || bl != 7 {
		t.Fatalf("at(1,1) = (%d,%d,%d), want (9,8,7)", r, g, bl)
	}
	// Just past the end of sub-rect 1 (x=4 w=4 -> covers 4..7; 8 is background).
	if r, g, bl := at(8, 2); r != 0 || g != 0 || bl != 0 {
		t.Fatalf("at(8,2) = (%d,%d,%d), want (0,0,0)", r, g, bl)
	}
}

// TestRREDispatchedFromRectangle: the wire rectangle header routes to RRE.
func TestRREDispatchedFromRectangle(t *testing.T) {
	b := be()
	// Rectangle header: x, y, w, h (2 bytes each) + encType (4 bytes, EncRRE=2).
	putBE16(b, 0) // x
	putBE16(b, 0) // y
	putBE16(b, 2) // w
	putBE16(b, 2) // h
	var encBuf [4]byte
	binary.BigEndian.PutUint32(encBuf[:], uint32(EncRRE))
	b.Write(encBuf[:])
	// RRE payload: 0 sub-rects, bg (5,6,7).
	b.Write([]byte{0, 0, 0, 0})
	hp(b, 5, 6, 7)

	c := newRREConn(b)
	rect := &Rectangle{}
	if err := rect.Read(c); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := rect.Enc.(*RREEncoding); !ok {
		t.Fatalf("rect.Enc = %T, want *RREEncoding", rect.Enc)
	}
	if len(rect.Enc.(*RREEncoding).Colors) != 4 {
		t.Fatalf("len(Colors) = %d, want 4", len(rect.Enc.(*RREEncoding).Colors))
	}
}
