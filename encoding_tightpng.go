package rfb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
)

func (*TightPngEncoding) Supported(Conn) bool {
	return true
}

func (enc *TightPngEncoding) Write(c Conn, rect *Rectangle) error {
	if err := writeTightCC(c, enc.TightCC); err != nil {
		return err
	}
	cmp := enc.TightCC.Compression
	switch cmp {
	case TightCompressionPNG:
		buf := bPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer bPool.Put(buf)
		pngEnc := &png.Encoder{CompressionLevel: png.BestSpeed}
		//pngEnc := &png.Encoder{CompressionLevel: png.NoCompression}
		if err := pngEnc.Encode(buf, enc.Image); err != nil {
			return err
		}
		if err := writeTightLength(c, buf.Len()); err != nil {
			return err
		}

		if _, err := buf.WriteTo(c); err != nil {
			return err
		}
	case TightCompressionFill:
		var tpx TightPixel
		r, g, b, _ := enc.Image.At(0, 0).RGBA()
		// RGBA returns 16-bit values (opaque alpha = 0xffff); take the high
		// byte to recover the 8-bit channel value.
		tpx.R = uint8(r >> 8)
		tpx.G = uint8(g >> 8)
		tpx.B = uint8(b >> 8)
		if err := binary.Write(c, binary.BigEndian, tpx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown tight compression %d", cmp)
	}
	return nil
}

type TightPngEncoding struct {
	TightCC *TightCC
	Image   image.Image
}

func (*TightPngEncoding) Type() EncodingType { return EncTightPng }

func (enc *TightPngEncoding) Read(c Conn, rect *Rectangle) error {
	tcc, err := readTightCC(c)
	if err != nil {
		return err
	}
	enc.TightCC = tcc
	cmp := enc.TightCC.Compression
	switch cmp {
	case TightCompressionPNG:
		l, err := readTightLength(c)
		if err != nil {
			return err
		}
		enc.Image, err = png.Decode(io.LimitReader(c, int64(l)))
		if err != nil {
			return err
		}
	case TightCompressionFill:
		var tpx TightPixel
		if err := binary.Read(c, binary.BigEndian, &tpx); err != nil {
			return err
		}
		enc.Image = image.NewRGBA(image.Rect(0, 0, 1, 1))
		enc.Image.(draw.Image).Set(0, 0, color.RGBA{R: tpx.R, G: tpx.G, B: tpx.B, A: 255})
	default:
		return fmt.Errorf("unknown compression %d", cmp)
	}
	return nil
}
