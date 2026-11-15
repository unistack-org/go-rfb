package rfb

import "encoding/binary"

// ExtendedDesktopSizeScreen describes one screen in a (possibly multi-head)
// screen configuration.
type ExtendedDesktopSizeScreen struct {
	ID     uint32
	X, Y   uint16
	Width  uint16
	Height uint16
	Flags  uint32
}

// ExtendedDesktopSizeEncoding represents the "ExtendedDesktopSize"
// pseudo-encoding.
//
// A client which requests this pseudo-encoding declares that it can cope with
// a change in the framebuffer width, height, and/or screen configuration. The
// new framebuffer size is carried in the rectangle header's width and height
// fields; the header's x-position is the reason for the change and the
// y-position is a status code (meaningful only when the change was requested
// by the client). After the header, a number-of-screens count, 3 bytes of
// reserved padding, and that many SCREEN structures follow.
type ExtendedDesktopSizeEncoding struct {
	// Reason is the rectangle header's x-position: 0 = changed via
	// non-RFB means on the server, 1 = requested by this client,
	// 2 = requested by another client, unknown values should be treated as 0.
	Reason uint16
	// Status is the rectangle header's y-position: a status code for a change
	// requested by a client (0 = no error), zero/ignored otherwise.
	Status  uint16
	Screens []ExtendedDesktopSizeScreen
}

func (*ExtendedDesktopSizeEncoding) Supported(Conn) bool {
	return true
}

func (*ExtendedDesktopSizeEncoding) Type() EncodingType {
	return EncExtendedDesktopSizePseudo
}

// Read implements the Encoding interface.
func (enc *ExtendedDesktopSizeEncoding) Read(c Conn, rect *Rectangle) error {
	enc.Reason = rect.X
	enc.Status = rect.Y

	var numScreens uint8
	if err := binary.Read(c, binary.BigEndian, &numScreens); err != nil {
		return err
	}
	// 3 reserved padding bytes follow the screen count.
	var pad [3]byte
	if err := binary.Read(c, binary.BigEndian, &pad); err != nil {
		return err
	}

	enc.Screens = make([]ExtendedDesktopSizeScreen, numScreens)
	for i := range enc.Screens {
		s := &enc.Screens[i]
		if err := binary.Read(c, binary.BigEndian, &s.ID); err != nil {
			return err
		}
		if err := binary.Read(c, binary.BigEndian, &s.X); err != nil {
			return err
		}
		if err := binary.Read(c, binary.BigEndian, &s.Y); err != nil {
			return err
		}
		if err := binary.Read(c, binary.BigEndian, &s.Width); err != nil {
			return err
		}
		if err := binary.Read(c, binary.BigEndian, &s.Height); err != nil {
			return err
		}
		if err := binary.Read(c, binary.BigEndian, &s.Flags); err != nil {
			return err
		}
	}
	return nil
}

// Write implements the Encoding interface.
func (enc *ExtendedDesktopSizeEncoding) Write(c Conn, rect *Rectangle) error {
	var pad [3]byte
	if err := binary.Write(c, binary.BigEndian, uint8(len(enc.Screens))); err != nil {
		return err
	}
	if err := binary.Write(c, binary.BigEndian, pad); err != nil {
		return err
	}
	for i := range enc.Screens {
		s := &enc.Screens[i]
		if err := binary.Write(c, binary.BigEndian, s.ID); err != nil {
			return err
		}
		if err := binary.Write(c, binary.BigEndian, s.X); err != nil {
			return err
		}
		if err := binary.Write(c, binary.BigEndian, s.Y); err != nil {
			return err
		}
		if err := binary.Write(c, binary.BigEndian, s.Width); err != nil {
			return err
		}
		if err := binary.Write(c, binary.BigEndian, s.Height); err != nil {
			return err
		}
		if err := binary.Write(c, binary.BigEndian, s.Flags); err != nil {
			return err
		}
	}
	return nil
}
