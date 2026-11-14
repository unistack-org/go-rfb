package rfb

// TranscodeAtenHermon rewrites an ATEN iKVM frame-buffer update into a
// standard VNC frame-buffer update that a plain RFB client can render.
//
// An ATEN "hermon" rectangle (EncType 0x59, or a raw rectangle on an ATEN
// protocol connection) is decoded as an AtenHermon: a mixture of 16x16
// RGB555 sub-rectangle tiles and full-rectangle raw payloads. A standard VNC
// client has no idea what encoding 0x59 is, so sending it through verbatim
// produces a broken or blank frame. This function expands each 16x16 tile
// into a normal raw rectangle (decoding the RGB555 little-endian pixel data
// into 8-bit R/G/B) and passes the full-rectangle raw payload through as a
// raw rectangle. Rectangles that are not hermon (CopyRect, plain raw, pseudo
// encodings, and the ATEN "screen is off" sentinel) are left for the caller.
//
// The returned update is a new FramebufferUpdate; the input is not mutated.
// A hermon rectangle whose children are all out of bounds or that carries no
// paintable payload (the screen-off sentinel) yields no rectangles.
func (fu *FramebufferUpdate) TranscodeAtenHermon() *FramebufferUpdate {
	if fu == nil {
		return nil
	}
	out := &FramebufferUpdate{}
	for _, rect := range fu.Rects {
		if rect == nil {
			continue
		}
		hermon, isHermon := rect.Enc.(*AtenHermon)
		if !isHermon {
			// Not an ATEN hermon payload: CopyRect, plain raw, pseudo
			// encodings, etc. Forward unchanged.
			out.Rects = append(out.Rects, rect)
			continue
		}
		// Drop the ATEN "screen is off" sentinel (no paintable children).
		if len(hermon.Encodings) == 0 {
			continue
		}
		for _, child := range hermon.Encodings {
			switch c := child.(type) {
			case *AtenHermonSubrect:
				const t = 16
				r := &Rectangle{
					X:       rect.X + uint16(c.X)*t,
					Y:       rect.Y + uint16(c.Y)*t,
					Width:   t,
					Height:  t,
					EncType: EncRaw,
					Enc:     &RawEncoding{Colors: decodeAtenSubrect(c)},
				}
				out.Rects = append(out.Rects, r)
			case *RawEncoding:
				// Full-rectangle raw payload: same layout as a normal raw
				// rect at the hermon rectangle's origin.
				r := &Rectangle{
					X:       rect.X,
					Y:       rect.Y,
					Width:   rect.Width,
					Height:  rect.Height,
					EncType: EncRaw,
					Enc:     &RawEncoding{Colors: c.Colors},
				}
				out.Rects = append(out.Rects, r)
			}
		}
	}
	out.NumRect = uint16(len(out.Rects))
	return out
}

// decodeAtenSubrect expands the 16x16 little-endian RGB555 pixel data of an
// ATEN hermon sub-rectangle into 8-bit R/G/B colors. It mirrors
// FrameBuffer.blitAtenSubrectLocked so the transcoded stream paints the same
// pixels the compositor would. A short or empty payload yields no colors
// (the sub-rectangle is dropped).
func decodeAtenSubrect(sr *AtenHermonSubrect) []Color {
	const tile = 16
	if len(sr.Data) < tile*tile*2 {
		return nil
	}
	colors := make([]Color, tile*tile)
	pf := PixelFormat32bit
	for i := range colors {
		px := uint16(sr.Data[i*2]) | uint16(sr.Data[i*2+1])<<8
		clr := NewColor(pf, nil)
		clr.R = uint16(((px >> 10) & 0x1f) * 255 / 31)
		clr.G = uint16(((px >> 5) & 0x1f) * 255 / 31)
		clr.B = uint16((px & 0x1f) * 255 / 31)
		colors[i] = *clr
	}
	return colors
}
