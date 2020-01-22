package rfb

import "encoding/binary"

type LedStatePseudoEncoding struct {
	State uint8
}

func (*LedStatePseudoEncoding) Supported(Conn) bool {
	return true
}

func (*LedStatePseudoEncoding) Type() EncodingType { return EncLedStatePseudo }

func (enc *LedStatePseudoEncoding) Read(c Conn, rect *Rectangle) error {
	if err := binary.Read(c, binary.BigEndian, &enc.State); err != nil {
		return err
	}
	return nil
}

func (enc *LedStatePseudoEncoding) Write(c Conn, rect *Rectangle) error {
	return binary.Write(c, binary.BigEndian, []byte{enc.State})
}
