package rfb

import "encoding/binary"

// RREEncoding represents a RRE (Rough & Ready Encoding) rectangle (RFB §8.6).
//
// The wire layout is: a uint32 sub-rectangle count, one background color, then
// per sub-rectangle a color and a 16-bit x/y/width/height (relative to the
// rectangle origin). Like RawEncoding and HextileEncoding, decoding stores the
// result in Colors in row-major order and reuses Color.Read so palette lookups
// and the ATEN RGB555 channel expansion are handled by the shared code path.
type RREEncoding struct {
	Colors []Color
}

func (*RREEncoding) Supported(Conn) bool { return true }

func (*RREEncoding) Type() EncodingType { return EncRRE }

// Write is a no-op; RRE is a server->client encoding.
func (enc *RREEncoding) Write(c Conn, rect *Rectangle) error { return nil }

// Read decodes an RRE rectangle into enc.Colors (row-major, len = area).
func (enc *RREEncoding) Read(c Conn, rect *Rectangle) error {
	pf := c.PixelFormat()
	cm := c.ColorMap()
	w, h := int(rect.Width), int(rect.Height)
	enc.Colors = make([]Color, w*h)

	// Wire order (RFB §8.6): uint32 nSubrects, then the background color,
	// then the sub-rectangles.
	var numSubRects uint32
	if err := binary.Read(c, binary.BigEndian, &numSubRects); err != nil {
		return err
	}

	// Read the whole-rectangle background color and fill with it.
	bg := NewColor(pf, &cm)
	if err := bg.Read(c); err != nil {
		return err
	}
	for i := range enc.Colors {
		enc.Colors[i] = *bg
	}

	setRect := func(sx, sy, sw, sh int, col *Color) {
		for y := sy; y < sy+sh; y++ {
			if y < 0 || y >= h {
				continue
			}
			for x := sx; x < sx+sw; x++ {
				if x < 0 || x >= w {
					continue
				}
				enc.Colors[y*w+x] = *col
			}
		}
	}

	// Read each sub-rectangle: color + x + y + width + height.
	for i := uint32(0); i < numSubRects; i++ {
		color := NewColor(pf, &cm)
		if err := color.Read(c); err != nil {
			return err
		}
		var x, y, rw, rh uint16
		if err := binary.Read(c, binary.BigEndian, &x); err != nil {
			return err
		}
		if err := binary.Read(c, binary.BigEndian, &y); err != nil {
			return err
		}
		if err := binary.Read(c, binary.BigEndian, &rw); err != nil {
			return err
		}
		if err := binary.Read(c, binary.BigEndian, &rh); err != nil {
			return err
		}
		setRect(int(x), int(y), int(rw), int(rh), color)
	}

	return nil
}
