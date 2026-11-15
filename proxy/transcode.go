package proxy

import (
	"image"
	"image/color"

	vnc "github.com/unistack-org/go-rfb"
)

// transcodeFBUpdate converts an upstream frame update (possibly
// hermon-encoded) into a standard-RFB frame update whose rectangles are
// raw.
//
// A nil result means the update carried no paintable pixels (e.g. the
// ATEN "screen off" sentinel) and should be dropped.
func transcodeFBUpdate(fu *vnc.FramebufferUpdate) *vnc.FramebufferUpdate {
	if fu == nil || len(fu.Rects) == 0 {
		return nil
	}
	out := fu.TranscodeAtenHermon()
	if out == nil || len(out.Rects) == 0 {
		return nil
	}
	return out
}

// fanOut delivers a transcoded (raw) frame update to every attached
// downstream client, re-encoding rectangles as tight (PNG / Fill) for
// clients that advertised the tightpng encoding.
func fanOut(p *Proxy, fu *vnc.FramebufferUpdate) {
	p.mu.Lock()
	clients := make([]*downstream, 0, len(p.clients))
	for d := range p.clients {
		clients = append(clients, d)
	}
	p.mu.Unlock()

	for _, d := range clients {
		var out *vnc.FramebufferUpdate
		if d.wantsTightPng() {
			out = reencodeTightPng(fu)
		} else {
			out = fu
		}
		select {
		case d.out <- out:
		case <-d.done:
		default:
			// Client is alive but its outbound buffer is full; drop this
			// frame for it rather than stalling the shared pump.
		}
	}
}

// wantsTightPng reports whether the client advertised tightpng.
func (d *downstream) wantsTightPng() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.encs[vnc.EncTightPng]
}

// reencodeTightPng builds a per-client copy of the frame update with
// every raw rectangle re-encoded as tight: a solid-color rectangle
// becomes a Fill rectangle (1 pixel), anything else becomes a PNG
// rectangle.
func reencodeTightPng(fu *vnc.FramebufferUpdate) *vnc.FramebufferUpdate {
	out := &vnc.FramebufferUpdate{
		Rects: make([]*vnc.Rectangle, 0, len(fu.Rects)),
	}
	for _, r := range fu.Rects {
		// A zero-area rectangle encodes to nothing; drop it (a tightpng
		// PNG rectangle cannot carry zero pixels).
		if r.Width == 0 || r.Height == 0 {
			continue
		}
		raw, ok := r.Enc.(*vnc.RawEncoding)
		if !ok {
			// Non-raw rectangle (e.g. copyrect): pass through.
			out.Rects = append(out.Rects, r)
			continue
		}
		out.Rects = append(out.Rects, &vnc.Rectangle{
			X:       r.X,
			Y:       r.Y,
			Width:   r.Width,
			Height:  r.Height,
			EncType: vnc.EncTightPng,
			Enc:     tightPngForColors(raw.Colors, r.Width, r.Height),
		})
	}
	out.NumRect = uint16(len(out.Rects))
	return out
}

// tightPngForColors encodes a raw color buffer as tight: Fill when every
// pixel is identical, PNG otherwise.
func tightPngForColors(colors []vnc.Color, w, h uint16) vnc.Encoding {
	n := int(w) * int(h)
	if n == 0 || len(colors) != n {
		// Empty rectangle (or length mismatch): nothing to index; a zero
		// pixel buffer would otherwise panic on colors[0].
		return pngEncoding(buildRGBAImage(colors, w, h))
	}

	single := colors[0]
	solid := true
	for _, c := range colors[1:] {
		if c.R != single.R || c.G != single.G || c.B != single.B {
			solid = false
			break
		}
	}

	if solid {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.SetRGBA(0, 0, color.RGBA{uint8(single.R), uint8(single.G), uint8(single.B), 255})
		return &vnc.TightPngEncoding{
			TightCC: &vnc.TightCC{
				Compression: vnc.TightCompressionFill,
				Filter:      vnc.TightFilterCopy,
			},
			Image: img,
		}
	}
	return pngEncoding(buildRGBAImage(colors, w, h))
}

// pngEncoding wraps an image as a tight PNG rectangle.
func pngEncoding(img image.Image) vnc.Encoding {
	return &vnc.TightPngEncoding{
		TightCC: &vnc.TightCC{
			Compression: vnc.TightCompressionPNG,
			Filter:      vnc.TightFilterCopy,
		},
		Image: img,
	}
}

// buildRGBAImage converts a buffer of vnc.Colors (8-bit R/G/B, no alpha)
// into an opaque image.RGBA.
func buildRGBAImage(colors []vnc.Color, w, h uint16) *image.RGBA {
	bounds := image.Rect(0, 0, int(w), int(h))
	img := image.NewRGBA(bounds)
	pix := img.Pix
	for i, c := range colors {
		if i >= len(pix)/4 {
			break
		}
		base := i * 4
		pix[base+0] = uint8(c.R)
		pix[base+1] = uint8(c.G)
		pix[base+2] = uint8(c.B)
		pix[base+3] = 255
	}
	return img
}
