package rfb

// ContinuousUpdatesEncoding represents the "ContinuousUpdates"
// pseudo-encoding.
//
// A client which requests the ContinuousUpdates pseudo-encoding declares that
// it wishes to use the EnableContinuousUpdates extension. The server must
// send an EndOfContinuousUpdates message the first time it sees a
// SetEncodings message carrying this pseudo-encoding, to inform the client
// that the extension is supported. There is no data after the rectangle
// header.
type ContinuousUpdatesEncoding struct{}

func (*ContinuousUpdatesEncoding) Supported(Conn) bool {
	return true
}

func (*ContinuousUpdatesEncoding) Type() EncodingType {
	return EncContinuousUpdatesPseudo
}

// Read implements the Encoding interface. There is no data after the
// rectangle header.
func (*ContinuousUpdatesEncoding) Read(c Conn, rect *Rectangle) error {
	return nil
}

// Write implements the Encoding interface. There is no data after the
// rectangle header.
func (*ContinuousUpdatesEncoding) Write(c Conn, rect *Rectangle) error {
	return nil
}
