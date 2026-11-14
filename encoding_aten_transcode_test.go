package rfb

import (
	"testing"
)

// solidSubrect builds an ATEN hermon sub-rectangle of 16x16 pixels at tile
// (x, y) whose 16-bit little-endian pixel value is px (RGB555 layout).
func solidSubrect(x, y uint8, px uint16) *AtenHermonSubrect {
	const tile = 16
	data := make([]byte, tile*tile*2)
	for i := 0; i < len(data)/2; i++ {
		data[i*2] = byte(px)
		data[i*2+1] = byte(px >> 8)
	}
	return &AtenHermonSubrect{X: x, Y: y, Data: data}
}

// TestTranscodeAtenHermonSubrect verifies that a hermon rectangle made of
// 16x16 sub-rectangles is expanded into raw rectangles positioned at
// (rect origin + tile*16) with the RGB555 data decoded to 8-bit.
func TestTranscodeAtenHermonSubrect(t *testing.T) {
	// Solid red tile (RGB555 0x7C00 -> R=255) at tile (2,1) of a 64x48 rect.
	sub := solidSubrect(2, 1, 0x7C00)
	fu := &FramebufferUpdate{
		NumRect: 1,
		Rects: []*Rectangle{{
			X: 0, Y: 0, Width: 64, Height: 48,
			EncType: EncAtenHermon,
			Enc:     &AtenHermon{Encodings: []Encoding{sub}},
		}},
	}

	got := fu.TranscodeAtenHermon()
	if got == nil {
		t.Fatal("TranscodeAtenHermon returned nil")
	}
	if len(got.Rects) != 1 {
		t.Fatalf("want 1 rect, got %d", len(got.Rects))
	}
	if got.NumRect != 1 {
		t.Fatalf("want NumRect 1, got %d", got.NumRect)
	}

	r := got.Rects[0]
	if r.X != 32 || r.Y != 16 || r.Width != 16 || r.Height != 16 {
		t.Fatalf("want rect at (32,16) 16x16, got (%d,%d) %dx%d", r.X, r.Y, r.Width, r.Height)
	}
	if r.EncType != EncRaw {
		t.Fatalf("want EncRaw, got %s", r.EncType)
	}
	raw, ok := r.Enc.(*RawEncoding)
	if !ok {
		t.Fatalf("want *RawEncoding, got %T", r.Enc)
	}
	if len(raw.Colors) != 256 {
		t.Fatalf("want 256 colors, got %d", len(raw.Colors))
	}
	// Every pixel of the tile is 8-bit red.
	for i, c := range raw.Colors {
		if c.R != 255 || c.G != 0 || c.B != 0 {
			t.Fatalf("color[%d] = (%d,%d,%d), want (255,0,0)", i, c.R, c.G, c.B)
		}
	}
}

// TestTranscodeAtenHermonRawChild verifies that a hermon full-rectangle raw
// child is emitted as a raw rectangle at the hermon rectangle's origin.
func TestTranscodeAtenHermonRawChild(t *testing.T) {
	// A 4x2 raw child carrying two green pixels.
	raw := &RawEncoding{Colors: []Color{
		{R: 0, G: 255, B: 0}, {R: 0, G: 200, B: 0},
		{R: 0, G: 100, B: 0}, {R: 0, G: 50, B: 0},
	}}
	fu := &FramebufferUpdate{
		NumRect: 1,
		Rects: []*Rectangle{{
			X: 10, Y: 20, Width: 4, Height: 2,
			EncType: EncAtenHermon,
			Enc:     &AtenHermon{Encodings: []Encoding{raw}},
		}},
	}

	got := fu.TranscodeAtenHermon()
	if len(got.Rects) != 1 {
		t.Fatalf("want 1 rect, got %d", len(got.Rects))
	}
	r := got.Rects[0]
	if r.X != 10 || r.Y != 20 || r.Width != 4 || r.Height != 2 {
		t.Fatalf("want rect at (10,20) 4x2, got (%d,%d) %dx%d", r.X, r.Y, r.Width, r.Height)
	}
	if r.EncType != EncRaw {
		t.Fatalf("want EncRaw, got %s", r.EncType)
	}
	gotRaw, ok := r.Enc.(*RawEncoding)
	if !ok {
		t.Fatalf("want *RawEncoding, got %T", r.Enc)
	}
	if len(gotRaw.Colors) != 4 {
		t.Fatalf("want 4 colors, got %d", len(gotRaw.Colors))
	}
	if gotRaw.Colors[3].G != 50 {
		t.Fatalf("want last pixel G=50, got %d", gotRaw.Colors[3].G)
	}
}

// TestTranscodeAtenHermonMixed verifies mixed children: a sub-rect tile and a
// raw block in the same hermon rectangle are both emitted, in order.
func TestTranscodeAtenHermonMixed(t *testing.T) {
	sub := solidSubrect(0, 0, 0x001F) // blue tile (B=31 -> 0x001F)
	raw := &RawEncoding{Colors: []Color{{R: 255, G: 255, B: 255}}}
	fu := &FramebufferUpdate{
		NumRect: 1,
		Rects: []*Rectangle{{
			X: 0, Y: 0, Width: 8, Height: 4,
			EncType: EncAtenHermon,
			Enc:     &AtenHermon{Encodings: []Encoding{sub, raw}},
		}},
	}

	got := fu.TranscodeAtenHermon()
	if len(got.Rects) != 2 {
		t.Fatalf("want 2 rects, got %d", len(got.Rects))
	}
	// First: the blue tile at (0,0) 16x16.
	if r0 := got.Rects[0]; r0.X != 0 || r0.Y != 0 || r0.Width != 16 || r0.Height != 16 {
		t.Fatalf("want tile at (0,0) 16x16, got (%d,%d) %dx%d", r0.X, r0.Y, r0.Width, r0.Height)
	}
	if gotRaw, ok := got.Rects[0].Enc.(*RawEncoding); ok {
		if gotRaw.Colors[0].B != 255 || gotRaw.Colors[0].R != 0 {
			t.Fatalf("want blue tile (0,0,255), got (%d,%d,%d)", gotRaw.Colors[0].R, gotRaw.Colors[0].G, gotRaw.Colors[0].B)
		}
	} else {
		t.Fatal("want *RawEncoding for subrect")
	}
	// Second: the raw block at the hermon origin (0,0) 8x4.
	if r1 := got.Rects[1]; r1.Width != 8 || r1.Height != 4 {
		t.Fatalf("want raw block 8x4, got %dx%d", r1.Width, r1.Height)
	}
}

// TestTranscodeAtenHermonScreenOff verifies the screen-off sentinel (hermon
// with no children) is dropped and produces no rectangles.
func TestTranscodeAtenHermonScreenOff(t *testing.T) {
	fu := &FramebufferUpdate{
		NumRect: 1,
		Rects: []*Rectangle{{
			// 64896 x 65056 == -640 x -480 (i16 underflow) is the ATEN
			// "screen is off" signal.
			X: 64896, Y: 65056, Width: 64896, Height: 65056,
			EncType: EncAtenHermon,
			Enc:     &AtenHermon{Encodings: nil},
		}},
	}

	got := fu.TranscodeAtenHermon()
	if len(got.Rects) != 0 {
		t.Fatalf("want 0 rects for screen-off, got %d", len(got.Rects))
	}
	if got.NumRect != 0 {
		t.Fatalf("want NumRect 0, got %d", got.NumRect)
	}
}

// TestTranscodeAtenHermonPassthrough verifies non-hermon rectangles are
// forwarded unchanged (same pointer).
func TestTranscodeAtenHermonPassthrough(t *testing.T) {
	copyRect := &Rectangle{
		X: 1, Y: 2, Width: 3, Height: 4,
		EncType: EncCopyRect,
		Enc:     &CopyRectEncoding{},
	}
	plainRaw := &Rectangle{
		X: 5, Y: 6, Width: 4, Height: 2,
		EncType: EncRaw,
		Enc:     &RawEncoding{Colors: []Color{{R: 1, G: 2, B: 3}}},
	}
	fu := &FramebufferUpdate{
		NumRect: 2,
		Rects:   []*Rectangle{copyRect, plainRaw},
	}

	got := fu.TranscodeAtenHermon()
	if len(got.Rects) != 2 {
		t.Fatalf("want 2 rects, got %d", len(got.Rects))
	}
	if got.Rects[0] != copyRect {
		t.Fatal("CopyRect rect was not forwarded unchanged")
	}
	if got.Rects[1] != plainRaw {
		t.Fatal("plain raw rect was not forwarded unchanged")
	}
}

// TestTranscodeAtenHermonNil verifies nil-safety.
func TestTranscodeAtenHermonNil(t *testing.T) {
	var fu *FramebufferUpdate
	if got := fu.TranscodeAtenHermon(); got != nil {
		t.Fatalf("want nil for nil input, got %v", got)
	}
	// A nil rect inside the list is skipped.
	got := (&FramebufferUpdate{NumRect: 1, Rects: []*Rectangle{nil}}).TranscodeAtenHermon()
	if len(got.Rects) != 0 {
		t.Fatalf("want 0 rects, got %d", len(got.Rects))
	}
}

// TestTranscodeAtenHermonShortData verifies a sub-rectangle with a short
// (malformed) payload is dropped rather than emitting a broken tile.
func TestTranscodeAtenHermonShortData(t *testing.T) {
	sub := &AtenHermonSubrect{X: 0, Y: 0, Data: []byte{0x00, 0x7C}} // only 1 pixel
	fu := &FramebufferUpdate{
		NumRect: 1,
		Rects: []*Rectangle{{
			X: 0, Y: 0, Width: 64, Height: 48,
			EncType: EncAtenHermon,
			Enc:     &AtenHermon{Encodings: []Encoding{sub}},
		}},
	}
	got := fu.TranscodeAtenHermon()
	if len(got.Rects) != 1 {
		t.Fatalf("want 1 rect, got %d", len(got.Rects))
	}
	raw := got.Rects[0].Enc.(*RawEncoding)
	if len(raw.Colors) != 0 {
		t.Fatalf("want 0 colors for short data, got %d", len(raw.Colors))
	}
}
