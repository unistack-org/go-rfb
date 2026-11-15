package rfb

// QEMUExtendedKeyEventEncoding represents the "QEMU Extended Key Event"
// pseudo-encoding.
//
// A client that supports this encoding indicates that it is able to provide
// raw key codes (scancodes) as an alternative to keysyms. If a server wishes
// to receive raw key codes it sends an empty pseudo-rectangle with this
// encoding; there is no data after the rectangle header. After receiving this
// notification, the client may send a QEMU Extended Key Event message in
// preference to the traditional Key Event message.
type QEMUExtendedKeyEventEncoding struct{}

func (*QEMUExtendedKeyEventEncoding) Supported(Conn) bool {
	return true
}

func (*QEMUExtendedKeyEventEncoding) Type() EncodingType {
	return EncQEMUExtendedKeyEventPseudo
}

// Read implements the Encoding interface. There is no data after the
// rectangle header.
func (*QEMUExtendedKeyEventEncoding) Read(c Conn, rect *Rectangle) error {
	return nil
}

// Write implements the Encoding interface. There is no data after the
// rectangle header.
func (*QEMUExtendedKeyEventEncoding) Write(c Conn, rect *Rectangle) error {
	return nil
}
