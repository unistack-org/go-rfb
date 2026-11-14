package rfb

import (
	"image"
	"image/draw"
	"sync"
)

// FrameBuffer is a client-side full-frame image that a user maintains by
// compositing each incoming FramebufferUpdate. It keeps a full *image.RGBA
// in memory and exposes the current frame as a draw.Image via Screenshot(),
// which is the building block for capturing a VNC screenshot.
//
// It mirrors the v2v11 VncCanvas/DrawImage model (a client-owned draw.Image
// target) but is driven by our flat []Color encodings, so it needs no changes
// to the Encoding interface.
//
// Usage from a client message loop:
//
//	fb := vnc.NewFrameBuffer(0, 0) // grows on the first resize
//	for msg := range cchServer {
//		if fu, ok := msg.(*vnc.FramebufferUpdate); ok {
//			fb.Composite(fu) // resizes on desktop-size rects, blits pixels
//		}
//	}
//	screenshot := fb.Screenshot() // draw.Image
//
// Thread safety: a single FrameBuffer may be composited in the read-loop
// goroutine and screenshotted in another; every public method takes the lock.
type FrameBuffer struct {
	mu     sync.Mutex
	width  int
	height int
	img    *image.RGBA
}

// NewFrameBuffer creates a FrameBuffer of the given size in pixels. A zero
// size is allowed; the buffer is resized by the first desktop-size update or
// an explicit Resize.
func NewFrameBuffer(width, height int) *FrameBuffer {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	return &FrameBuffer{
		width:  width,
		height: height,
		img:    image.NewRGBA(image.Rect(0, 0, width, height)),
	}
}

// Width returns the frame buffer width in pixels.
func (fb *FrameBuffer) Width() int {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	return fb.width
}

// Height returns the frame buffer height in pixels.
func (fb *FrameBuffer) Height() int {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	return fb.height
}

// Screenshot returns a draw.Image of the current full frame.
//
// The returned value is a copy: later composites to this FrameBuffer do not
// affect it. A zero-sized FrameBuffer returns an empty (zero-rect) image.
func (fb *FrameBuffer) Screenshot() draw.Image {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.width == 0 || fb.height == 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}
	dst := image.NewRGBA(fb.img.Bounds())
	draw.Draw(dst, dst.Bounds(), fb.img, image.Point{}, draw.Src)
	return dst
}

// Resize resizes the frame buffer, keeping the top-left overlap and clearing
// any newly exposed region.
func (fb *FrameBuffer) Resize(width, height int) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	fb.resizeLocked(width, height)
}

// Composite folds one decoded FramebufferUpdate into the frame buffer.
//
// It handles the encodings this library can decode into pixel data or a copy
// operation: Raw, Hextile and RRE (via their Colors slice), CopyRect (screen
// copy), the ATEN hermon sub-rectangle + raw mix, and desktop-size pseudo
// updates (resize). Cursor, desktop-name, LED-state and other pseudo
// encodings are ignored.
//
// Out-of-bounds geometry is clamped rather than panicking.
func (fb *FrameBuffer) Composite(fu *FramebufferUpdate) {
	if fu == nil {
		return
	}
	fb.mu.Lock()
	defer fb.mu.Unlock()
	for _, rect := range fu.Rects {
		if rect == nil {
			continue
		}
		fb.compositeRectLocked(rect)
	}
}

// compositeRectLocked is the per-rectangle dispatch. Callers must hold
// fb.mu.
func (fb *FrameBuffer) compositeRectLocked(rect *Rectangle) {
	// ATEN "raw"/hermon: on an ATEN protocol a raw rect is decoded as an
	// AtenHermon, so check the concrete type before the encoding type.
	if _, isHermon := rect.Enc.(*AtenHermon); isHermon {
		fb.compositeAtenLocked(rect)
		return
	}

	switch rect.EncType {
	case EncDesktopSizePseudo:
		// The pseudo encoding carries the new size in the rect's
		// Width/Height fields.
		fb.resizeLocked(int(rect.Width), int(rect.Height))

	case EncCopyRect:
		if enc, ok := rect.Enc.(*CopyRectEncoding); ok {
			fb.copyRectLocked(rect, enc)
		}

	case EncRaw, EncHextile, EncRRE:
		switch e := rect.Enc.(type) {
		case *RawEncoding:
			fb.blitLocked(int(rect.X), int(rect.Y), int(rect.Width), int(rect.Height), e.Colors)
		case *HextileEncoding:
			fb.blitLocked(int(rect.X), int(rect.Y), int(rect.Width), int(rect.Height), e.Colors)
		case *RREEncoding:
			fb.blitLocked(int(rect.X), int(rect.Y), int(rect.Width), int(rect.Height), e.Colors)
		}
	}
}

// compositeAtenLocked composites an ATEN hermon update. 16x16 sub-rectangles
// are placed at (X*16, Y*16) — X/Y are tile indices, not pixels — and
// full-width raw blocks are painted at the rect's origin.
func (fb *FrameBuffer) compositeAtenLocked(rect *Rectangle) {
	for _, child := range rect.Enc.(*AtenHermon).Encodings {
		switch c := child.(type) {
		case *AtenHermonSubrect:
			fb.blitAtenSubrectLocked(rect, c)
		case *RawEncoding:
			// Full-width raw block: same layout as a normal raw rect.
			fb.blitLocked(int(rect.X), int(rect.Y), int(rect.Width), int(rect.Height), c.Colors)
		}
	}
}

// blitAtenSubrectLocked decodes a 16x16 RGB555 little-endian sub-rectangle
// and blits it at (rect.X + X*16, rect.Y + Y*16). Callers must hold fb.mu.
func (fb *FrameBuffer) blitAtenSubrectLocked(rect *Rectangle, sr *AtenHermonSubrect) {
	const tile = 16
	if len(sr.Data) < tile*tile*2 {
		return
	}
	colors := make([]Color, tile*tile)
	for i := range colors {
		px := uint16(sr.Data[i*2]) | uint16(sr.Data[i*2+1])<<8
		colors[i] = Color{
			R: uint16(((px >> 10) & 0x1f) * 255 / 31),
			G: uint16(((px >> 5) & 0x1f) * 255 / 31),
			B: uint16((px & 0x1f) * 255 / 31),
		}
	}
	fb.blitLocked(int(rect.X)+int(sr.X)*tile, int(rect.Y)+int(sr.Y)*tile, tile, tile, colors)
}

// blitLocked copies a row-major Colors slice into the frame with its top-left
// at (x, y). Any part of the source that falls outside the frame is dropped.
// Callers must hold fb.mu.
func (fb *FrameBuffer) blitLocked(x, y, w, h int, colors []Color) {
	if w <= 0 || h <= 0 || len(colors) < w*h {
		return
	}
	dst := image.Rect(x, y, x+w, y+h)
	ov := inter(dst, fb.img.Bounds())
	if ov.Empty() {
		return
	}
	// Local source coordinates of ov.Min within the rect.
	localX := ov.Min.X - x
	localY := ov.Min.Y - y
	for dy := 0; dy < ov.Dy(); dy++ {
		for dx := 0; dx < ov.Dx(); dx++ {
			ci := (localY+dy)*w + (localX + dx)
			fb.setPixelLocked(ov.Min.X+dx, ov.Min.Y+dy, colors[ci])
		}
	}
}

// copyRectLocked performs a screen copy: per the VNC spec, destination point
// p receives the pixel currently at (SX + p.x - dst.Min.x, SY + p.y -
// dst.Min.y), where dst is (rect.X, rect.Y, w, h). Callers must hold fb.mu.
func (fb *FrameBuffer) copyRectLocked(rect *Rectangle, enc *CopyRectEncoding) {
	dst := image.Rect(int(rect.X), int(rect.Y), int(rect.X)+int(rect.Width), int(rect.Y)+int(rect.Height))
	dstOv := inter(dst, fb.img.Bounds())
	if dstOv.Empty() {
		return
	}
	// Source region corresponding to dstOv under the mapping above.
	sMin := image.Point{
		int(enc.SX) + dstOv.Min.X - int(rect.X),
		int(enc.SY) + dstOv.Min.Y - int(rect.Y),
	}
	src := image.Rect(sMin.X, sMin.Y, sMin.X+dstOv.Dx(), sMin.Y+dstOv.Dy())
	sClipped := inter(src, fb.img.Bounds())
	if sClipped.Empty() {
		return
	}
	// Destination sub-rect that receives the clipped source.
	dClipped := image.Rect(
		sClipped.Min.X-int(enc.SX)+int(rect.X), sClipped.Min.Y-int(enc.SY)+int(rect.Y),
		sClipped.Min.X-int(enc.SX)+int(rect.X)+sClipped.Dx(), sClipped.Min.Y-int(enc.SY)+int(rect.Y)+sClipped.Dy(),
	)
	if !inter(dClipped, sClipped).Empty() {
		// Stage the source when it overlaps the destination, so an in-place
		// copy does not corrupt the source region it reads from.
		stage := image.NewRGBA(sClipped)
		draw.Draw(stage, sClipped, fb.img, sClipped.Min, draw.Src)
		draw.Draw(fb.img, dClipped, stage, sClipped.Min, draw.Src)
		return
	}
	draw.Draw(fb.img, dClipped, fb.img, sClipped.Min, draw.Src)
}

// resizeLocked resizes the frame, keeping the top-left overlap. Callers must
// hold fb.mu.
func (fb *FrameBuffer) resizeLocked(width, height int) {
	if width < 0 || height < 0 {
		return
	}
	if width == fb.width && height == fb.height {
		return
	}
	next := image.NewRGBA(image.Rect(0, 0, width, height))
	if ov := inter(fb.img.Bounds(), next.Bounds()); !ov.Empty() {
		draw.Draw(next, ov, fb.img, ov.Min, draw.Src)
	}
	fb.width = width
	fb.height = height
	fb.img = next
}

// inter returns the intersection of two rectangles (or an empty rectangle).
// It is a local stand-in for image.Rectangle.Intersect.
//
// The result is constructed directly rather than via image.Rect: image.Rect
// swaps a flipped rectangle's min/max, which would turn a disjoint (empty)
// intersection into a large non-empty one, so we must preserve the flipped
// bounds to keep the empty result.
func inter(a, b image.Rectangle) image.Rectangle {
	return image.Rectangle{
		Min: image.Point{max(a.Min.X, b.Min.X), max(a.Min.Y, b.Min.Y)},
		Max: image.Point{min(a.Max.X, b.Max.X), min(a.Max.Y, b.Max.Y)},
	}
}

// setPixelLocked writes one color into the frame at (x, y). Callers must hold
// fb.mu and guarantee (x, y) is inside the frame.
func (fb *FrameBuffer) setPixelLocked(x, y int, c Color) {
	i := fb.img.PixOffset(x, y)
	fb.img.Pix[i+0] = byte(c.R)
	fb.img.Pix[i+1] = byte(c.G)
	fb.img.Pix[i+2] = byte(c.B)
	fb.img.Pix[i+3] = 0xff
}
