package rfb

// LastRectPseudoEncoding represents the "LastRect" pseudo-encoding.
//
// A client which requests the LastRect pseudo-encoding declares that it does
// not need the exact number of rectangles in a FramebufferUpdate message: it
// stops parsing when it reaches a rectangle carrying this encoding. A server
// may therefore start transmitting a FramebufferUpdate before it knows how
// many rectangles it will send, typically advertising 65535 rectangles and
// then terminating with a LastRect rectangle. There is no further data
// associated with the pseudo-rectangle.
type LastRectPseudoEncoding struct{}

func (*LastRectPseudoEncoding) Supported(Conn) bool {
	return true
}

func (*LastRectPseudoEncoding) Type() EncodingType {
	return EncLastRectPseudo
}

// Read implements the Encoding interface. There is no data after the
// rectangle header.
func (*LastRectPseudoEncoding) Read(c Conn, rect *Rectangle) error {
	return nil
}

// Write implements the Encoding interface. There is no data after the
// rectangle header.
func (*LastRectPseudoEncoding) Write(c Conn, rect *Rectangle) error {
	return nil
}
