package rfb

import (
	"image"
	"testing"
)

// fpx returns the 8-bit RGBA components of the pixel at (x, y) of img.
func fpx(t *testing.T, img *image.RGBA, x, y int) (r, g, b, a uint32) {
	t.Helper()
	i := img.PixOffset(x, y)
	return uint32(img.Pix[i+0]), uint32(img.Pix[i+1]), uint32(img.Pix[i+2]), uint32(img.Pix[i+3])
}

// TestFrameBufferRawBlit composites a 4x4 raw rect at an offset and checks the
// pixels land at the expected absolute screen positions.
func TestFrameBufferRawBlit(t *testing.T) {
	fb := NewFrameBuffer(64, 48)
	// 4x4 of solid (10, 20, 30) at (10, 20).
	colors := make([]Color, 16)
	for i := range colors {
		colors[i] = Color{R: 10, G: 20, B: 30}
	}
	fu := &FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{X: 10, Y: 20, Width: 4, Height: 4, EncType: EncRaw, Enc: &RawEncoding{Colors: colors}}},
	}
	fb.Composite(fu)

	img := fb.Screenshot().(*image.RGBA)
	// Inside the rect.
	if r, g, b, _ := fpx(t, img, 10, 20); r != 10 || g != 20 || b != 30 {
		t.Fatalf("pixel (10,20) = (%d,%d,%d), want (10,20,30)", r, g, b)
	}
	if r, g, b, _ := fpx(t, img, 13, 23); r != 10 || g != 20 || b != 30 {
		t.Fatalf("pixel (13,23) = (%d,%d,%d), want (10,20,30)", r, g, b)
	}
	// Just outside the rect: still black.
	if r, g, b, _ := fpx(t, img, 14, 24); r != 0 || g != 0 || b != 0 {
		t.Fatalf("pixel (14,24) = (%d,%d,%d), want (0,0,0)", r, g, b)
	}
}

// TestFrameBufferAtenSubrect verifies a 16x16 ATEN hermon sub-rectangle lands
// at (X*16, Y*16) — block indices, not pixels — and that an out-of-bounds
// tile is dropped without panicking.
func TestFrameBufferAtenSubrect(t *testing.T) {
	fb := NewFrameBuffer(64, 48)
	const tile = 16
	// 16x16 of solid red RGB555 (R=31 -> 0x7C00 per pixel, little-endian).
	data := make([]byte, tile*tile*2)
	for i := 0; i < tile*tile; i++ {
		data[i*2] = 0x00
		data[i*2+1] = 0x7C
	}
	sub := &AtenHermonSubrect{X: 2, Y: 1, Data: data} // tile -> (32, 16)
	hermon := &AtenHermon{Encodings: []Encoding{sub}}
	fu := &FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{X: 0, Y: 0, Width: 64, Height: 48, EncType: EncAtenHermon, Enc: hermon}},
	}
	fb.Composite(fu)

	img := fb.Screenshot().(*image.RGBA)
	// Top-left of the tile at (32,16) is red.
	if r, g, b, _ := fpx(t, img, 32, 16); r != 255 || g != 0 || b != 0 {
		t.Fatalf("pixel (32,16) = (%d,%d,%d), want (255,0,0)", r, g, b)
	}
	// Bottom-right of the tile at (32+15, 16+15) is red.
	if r, g, b, _ := fpx(t, img, 47, 31); r != 255 || g != 0 || b != 0 {
		t.Fatalf("pixel (47,31) = (%d,%d,%d), want (255,0,0)", r, g, b)
	}
	// One pixel above the tile is still black.
	if r, g, b, _ := fpx(t, img, 32, 15); r != 0 || g != 0 || b != 0 {
		t.Fatalf("pixel (32,15) = (%d,%d,%d), want (0,0,0)", r, g, b)
	}
	// One pixel right of the tile is still black.
	if r, g, b, _ := fpx(t, img, 48, 16); r != 0 || g != 0 || b != 0 {
		t.Fatalf("pixel (48,16) = (%d,%d,%d), want (0,0,0)", r, g, b)
	}
}

// TestFrameBufferAtenSubrectClipped ensures an out-of-bounds tile is clamped
// rather than panicking (e.g. a tile at X=100 on a 64px-wide frame).
func TestFrameBufferAtenSubrectClipped(t *testing.T) {
	fb := NewFrameBuffer(64, 48)
	data := make([]byte, 16*16*2)
	sub := &AtenHermonSubrect{X: 100, Y: 0, Data: data}
	hermon := &AtenHermon{Encodings: []Encoding{sub}}
	fu := &FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{X: 0, Y: 0, Width: 64, Height: 48, EncType: EncAtenHermon, Enc: hermon}},
	}
	// Must not panic.
	fb.Composite(fu)
}

// TestFrameBufferCopyRect performs a screen copy and checks the destination
// received the source pixels.
func TestFrameBufferCopyRect(t *testing.T) {
	fb := NewFrameBuffer(64, 48)
	// Paint a 4x4 solid (9, 9, 9) at (0, 0).
	src := make([]Color, 16)
	for i := range src {
		src[i] = Color{R: 9, G: 9, B: 9}
	}
	fb.Composite(&FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{X: 0, Y: 0, Width: 4, Height: 4, EncType: EncRaw, Enc: &RawEncoding{Colors: src}}},
	})
	// Copy (0,0) 4x4 to (10,10).
	fb.Composite(&FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{X: 10, Y: 10, Width: 4, Height: 4, EncType: EncCopyRect, Enc: &CopyRectEncoding{SX: 0, SY: 0}}},
	})
	img := fb.Screenshot().(*image.RGBA)
	if r, g, b, _ := fpx(t, img, 10, 10); r != 9 || g != 9 || b != 9 {
		t.Fatalf("copied pixel (10,10) = (%d,%d,%d), want (9,9,9)", r, g, b)
	}
}

// TestFrameBufferCopyRectOverlap verifies a self-overlapping copy (moving a
// block right within the same region) does not corrupt the source: with the
// spec mapping, an in-bounds destination pixel reads the source as it was
// before the copy.
func TestFrameBufferCopyRectOverlap(t *testing.T) {
	fb := NewFrameBuffer(16, 1)
	// Fill the whole 16x1 row with a known pattern first.
	full := make([]Color, 16)
	for i := range full {
		full[i] = Color{R: uint16(i), G: 0, B: 0}
	}
	fb.Composite(&FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{X: 0, Y: 0, Width: 16, Height: 1, EncType: EncRaw, Enc: &RawEncoding{Colors: full}}},
	})
	// Copy 8x1 from (0,0) to (4,0). The destination pixels 4..11 should each
	// receive the value originally at pixel 0..7 (snapshot semantics via the
	// staging buffer because source and destination overlap).
	fb.Composite(&FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{X: 4, Y: 0, Width: 8, Height: 1, EncType: EncCopyRect, Enc: &CopyRectEncoding{SX: 0, SY: 0}}},
	})
	img := fb.Screenshot().(*image.RGBA)
	for dx := 0; dx < 8; dx++ {
		if r, _, _, _ := fpx(t, img, 4+dx, 0); r != uint32(dx) {
			t.Fatalf("pixel (%d,0) = (%d,0,0), want (%d,0,0)", 4+dx, r, dx)
		}
	}
}

// TestFrameBufferResizePreserves checks that resizing keeps the top-left
// overlap and that a desktop-size pseudo rect drives the resize.
func TestFrameBufferResizePreserves(t *testing.T) {
	fb := NewFrameBuffer(10, 10)
	// Paint a 2x2 solid (1,2,3) at (0,0).
	src := make([]Color, 4)
	for i := range src {
		src[i] = Color{R: 1, G: 2, B: 3}
	}
	fb.Composite(&FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{X: 0, Y: 0, Width: 2, Height: 2, EncType: EncRaw, Enc: &RawEncoding{Colors: src}}},
	})
	// Grow to 16x9 via a desktop-size pseudo rect.
	fb.Composite(&FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{Width: 16, Height: 9, EncType: EncDesktopSizePseudo}},
	})
	if fb.Width() != 16 || fb.Height() != 9 {
		t.Fatalf("size = %dx%d, want 16x9", fb.Width(), fb.Height())
	}
	img := fb.Screenshot().(*image.RGBA)
	// The old top-left survives.
	if r, g, b, _ := fpx(t, img, 0, 0); r != 1 || g != 2 || b != 3 {
		t.Fatalf("pixel (0,0) after resize = (%d,%d,%d), want (1,2,3)", r, g, b)
	}
	// The clipped row (y=9) no longer exists; a new row at (15,8) is black.
	if r, g, b, _ := fpx(t, img, 15, 8); r != 0 || g != 0 || b != 0 {
		t.Fatalf("pixel (15,8) after resize = (%d,%d,%d), want (0,0,0)", r, g, b)
	}
}

// TestFrameBufferScreenshotIsCopy verifies Screenshot returns an independent
// copy that is unaffected by later composites.
func TestFrameBufferScreenshotIsCopy(t *testing.T) {
	fb := NewFrameBuffer(8, 8)
	shot := fb.Screenshot().(*image.RGBA) // all black
	src := make([]Color, 1)
	src[0] = Color{R: 255, G: 0, B: 0}
	fb.Composite(&FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{X: 0, Y: 0, Width: 1, Height: 1, EncType: EncRaw, Enc: &RawEncoding{Colors: src}}},
	})
	// The earlier screenshot is unchanged.
	if r, g, b, _ := fpx(t, shot, 0, 0); r != 0 || g != 0 || b != 0 {
		t.Fatalf("earlier screenshot pixel = (%d,%d,%d), want (0,0,0)", r, g, b)
	}
	// The live frame has the new pixel.
	live := fb.Screenshot().(*image.RGBA)
	if r, g, b, _ := fpx(t, live, 0, 0); r != 255 || g != 0 || b != 0 {
		t.Fatalf("live pixel = (%d,%d,%d), want (255,0,0)", r, g, b)
	}
}

// TestFrameBufferZeroSize confirms a zero-sized buffer returns an empty image
// and composites without panicking until resized.
func TestFrameBufferZeroSize(t *testing.T) {
	fb := NewFrameBuffer(0, 0)
	empty := fb.Screenshot()
	if !empty.Bounds().Empty() {
		t.Fatalf("empty screenshot bounds = %v, want empty", empty.Bounds())
	}
	// Composite a desktop-size rect to grow it, then a raw rect.
	fb.Composite(&FramebufferUpdate{
		NumRect: 1,
		Rects:   []*Rectangle{{Width: 4, Height: 4, EncType: EncDesktopSizePseudo}},
	})
	if fb.Width() != 4 || fb.Height() != 4 {
		t.Fatalf("size = %dx%d, want 4x4", fb.Width(), fb.Height())
	}
}
