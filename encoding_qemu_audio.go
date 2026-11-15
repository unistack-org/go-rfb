package rfb

// QEMUAudioEncoding represents the "QEMU Audio" pseudo-encoding.
//
// A client that supports this encoding indicates that it is able to receive
// an audio data stream. If a server wishes to send audio data it sends an
// empty pseudo-rectangle with this encoding; there is no data after the
// rectangle header. After receiving this notification, the client may use the
// QEMU Audio messages to receive the stream.
type QEMUAudioEncoding struct{}

func (*QEMUAudioEncoding) Supported(Conn) bool {
	return true
}

func (*QEMUAudioEncoding) Type() EncodingType {
	return EncQEMUAudioPseudo
}

// Read implements the Encoding interface. There is no data after the
// rectangle header.
func (*QEMUAudioEncoding) Read(c Conn, rect *Rectangle) error {
	return nil
}

// Write implements the Encoding interface. There is no data after the
// rectangle header.
func (*QEMUAudioEncoding) Write(c Conn, rect *Rectangle) error {
	return nil
}
