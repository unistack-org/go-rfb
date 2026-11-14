package rfb

import "encoding/binary"

// Hextile sub-encoding flags (RFB §8.7).
const (
	hextileRaw                 uint8 = 1
	hextileBackgroundSpecified uint8 = 2
	hextileForegroundSpecified uint8 = 4
	hextileAnySubrects         uint8 = 8
	hextileSubrectsColored     uint8 = 16
)

// HextileEncoding represents a hextile-encoded rectangle (RFB §8.7).
//
// The wire layout is a 16x16 tile grid over the rectangle; each tile carries a
// one-byte sub-encoding mask, then per-mask payload (raw pixels, bg/fg colors
// and colored sub-rects). Like RawEncoding, decoding stores the result in
// Colors in row-major order, reusing Color.Read so palette lookups and the
// ATEN RGB555 channel expansion are handled by the shared code path.
type HextileEncoding struct {
	Colors []Color
}

func (*HextileEncoding) Supported(Conn) bool { return true }

func (*HextileEncoding) Type() EncodingType { return EncHextile }

// Write is a no-op; hextile is a server->client encoding.
func (enc *HextileEncoding) Write(c Conn, rect *Rectangle) error { return nil }

// Read decodes a hextile rectangle into enc.Colors (row-major, len = area).
func (enc *HextileEncoding) Read(c Conn, rect *Rectangle) error {
	pf := c.PixelFormat()
	cm := c.ColorMap()
	w, h := int(rect.Width), int(rect.Height)
	enc.Colors = make([]Color, w*h)

	// Baseline: every pixel starts as the zero color (black for truecolor,
	// palette index 0 otherwise) and is overwritten as tiles decode.
	black := NewColor(pf, &cm)
	for i := range enc.Colors {
		enc.Colors[i] = *black
	}

	// bg/fg are carried across tiles and read lazily on first use.
	var bg, fg *Color
	readColor := func(dst **Color) error {
		col := NewColor(pf, &cm)
		if err := col.Read(c); err != nil {
			return err
		}
		*dst = col
		return nil
	}
	setRect := func(sx, sy, sw, sh int, col *Color) {
		for y := sy; y < sy+sh; y++ {
			for x := sx; x < sx+sw; x++ {
				enc.Colors[y*w+x] = *col
			}
		}
	}

	for ty := 0; ty < h; ty += 16 {
		th := 16
		if h-ty < 16 {
			th = h - ty
		}
		for tx := 0; tx < w; tx += 16 {
			tw := 16
			if w-tx < 16 {
				tw = w - tx
			}

			var sub uint8
			if err := binary.Read(c, binary.BigEndian, &sub); err != nil {
				return err
			}

			// RAW: read tw*th pixels directly.
			if sub&hextileRaw != 0 {
				for y := ty; y < ty+th; y++ {
					for x := tx; x < tx+tw; x++ {
						col := NewColor(pf, &cm)
						if err := col.Read(c); err != nil {
							return err
						}
						enc.Colors[y*w+x] = *col
					}
				}
				continue
			}

			if sub&hextileBackgroundSpecified != 0 {
				if err := readColor(&bg); err != nil {
					return err
				}
			}
			if sub&hextileForegroundSpecified != 0 {
				if err := readColor(&fg); err != nil {
					return err
				}
			}

			// Fill the tile with the background.
			if bg != nil {
				setRect(tx, ty, tw, th, bg)
			}

			// No sub-rects: done with this tile.
			if sub&hextileAnySubrects == 0 {
				continue
			}

			colored := sub&hextileSubrectsColored != 0
			var n uint8
			if err := binary.Read(c, binary.BigEndian, &n); err != nil {
				return err
			}
			for i := 0; i < int(n); i++ {
				var uImg *Color
				if colored {
					if err := readColor(&uImg); err != nil {
						return err
					}
				} else {
					uImg = fg
				}
				if uImg == nil {
					uImg = black
				}

				var b0, b1 uint8
				if err := binary.Read(c, binary.BigEndian, &b0); err != nil {
					return err
				}
				if err := binary.Read(c, binary.BigEndian, &b1); err != nil {
					return err
				}
				sx := tx + int(b0>>4)
				sy := ty + int(b0&0xF)
				sw := int(b1>>4) + 1
				sh := int(b1&0xF) + 1
				setRect(sx, sy, sw, sh, uImg)
			}
		}
	}
	return nil
}
