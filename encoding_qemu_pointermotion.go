package rfb

// QEMUPointerMotionChangeEncoding represents the "QEMU Pointer Motion Change"
// pseudo-encoding.
//
// A client that supports this encoding declares that it is able to send
// pointer events either as absolute coordinates or as relative deltas. The
// server switches the client between the two modes by sending a rectangle
// with this encoding. The mode is carried in the rectangle header's
// x-position field: 1 requests absolute coordinates (the RFB protocol default
// when this encoding is not supported) and 0 requests relative deltas. There
// is no data after the rectangle header.
type QEMUPointerMotionChangeEncoding struct{}

func (*QEMUPointerMotionChangeEncoding) Supported(Conn) bool {
	return true
}

func (*QEMUPointerMotionChangeEncoding) Type() EncodingType {
	return EncQEMUPointerMotionChangePseudo
}

// Read implements the Encoding interface. The mode is conveyed by the
// rectangle header's x-position (0 = relative deltas, 1 = absolute
// coordinates); there is no trailing data.
func (*QEMUPointerMotionChangeEncoding) Read(c Conn, rect *Rectangle) error {
	return nil
}

// Write implements the Encoding interface. The mode is conveyed by the
// rectangle header's x-position; there is no trailing data.
func (*QEMUPointerMotionChangeEncoding) Write(c Conn, rect *Rectangle) error {
	return nil
}
