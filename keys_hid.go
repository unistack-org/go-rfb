package rfb

// This file ports the X11-keysym-to-USB-HID-usage table used by ATEN iKVM
// for keyboard input. The mapping is derived from the noVNC bmc-support
// forks (mkrasselt1/supermicro-kvm-html5 `keysym2hid.js`, itself based on
// kelleyk). Only the byte/numeric mapping is ported; no code from the
// MPL-2.0 forks is vendored.
//
// ATEN iKVM expects USB-HID usage codes in key events, not X11 keysyms.

func buildKeysym2HID() map[Key]uint8 {
	m := make(map[Key]uint8)

	// F1-F12
	for i := F1; i <= F12; i++ {
		m[i] = 0x3a + uint8(i-F1)
	}

	// A-Z and a-z (a-z share the same HID code as A-Z)
	for i := A; i <= Z; i++ {
		m[i] = 0x04 + uint8(i-A)
		m[i+(SmallA-A)] = 0x04 + uint8(i-A)
	}

	// 1-9
	for i := Digit1; i <= Digit9; i++ {
		m[i] = 0x1e + uint8(i-Digit1)
	}

	m[Digit0] = 0x27
	m[Return] = 0x28
	m[Escape] = 0x29
	m[BackSpace] = 0x2a
	m[Tab] = 0x2b
	m[Space] = 0x2c
	m[Minus] = 0x2d
	m[Equal] = 0x2e
	m[BracketLeft] = 0x2f
	m[BracketRight] = 0x30
	m[Backslash] = 0x31
	m[Semicolon] = 0x33
	m[Apostrophe] = 0x34
	m[Grave] = 0x35
	m[Comma] = 0x36
	m[Period] = 0x37
	m[Slash] = 0x38

	m[Print] = 0x46
	m[ScrollLock] = 0x47
	m[Pause] = 0x48
	m[Insert] = 0x49
	m[Home] = 0x4a
	m[PageUp] = 0x4b
	m[Delete] = 0x4c
	m[End] = 0x4d
	m[PageDown] = 0x4e
	m[Right] = 0x4f
	m[Left] = 0x50
	m[Down] = 0x51
	m[Up] = 0x52

	m[ControlLeft] = 0xe0
	m[ControlRight] = m[ControlLeft]
	m[ShiftLeft] = 0xe1
	m[ShiftRight] = m[ShiftLeft]
	m[AltLeft] = 0xe2
	m[AltRight] = m[AltLeft]
	m[SuperLeft] = 0xe3
	m[SuperRight] = m[SuperLeft]

	m[CapsLock] = 0x39
	m[NumLock] = 0x53

	// Shifted characters map to the same HID code as the base key.
	m[Less] = m[Comma]
	m[Greater] = m[Period]
	m[Exclaim] = m[Digit1]
	m[At] = m[Digit2]
	m[NumberSign] = m[Digit3]
	m[Dollar] = m[Digit4]
	m[Percent] = m[Digit5]
	m[AsciiCircum] = m[Digit6]
	m[Ampersand] = m[Digit7]
	m[Asterisk] = m[Digit8]
	m[ParenLeft] = m[Digit9]
	m[ParenRight] = m[Digit0]
	m[Underscore] = m[Minus]
	m[Plus] = m[Equal]
	m[BraceLeft] = m[BracketLeft]
	m[BraceRight] = m[BracketRight]
	m[Bar] = m[Backslash]
	m[Colon] = m[Semicolon]
	m[QuoteDbl] = m[Apostrophe]
	m[AsciiTilde] = m[Grave]
	m[Question] = m[Slash]

	return m
}

var keysym2HID = buildKeysym2HID()

// KeysymToHID maps an X11 keysym to a USB-HID usage code for ATEN iKVM.
// The bool result is false when the keysym has no ATEN mapping; the caller
// should drop the event in that case (mirrors the bmc-support forks).
func KeysymToHID(k Key) (uint8, bool) {
	hid, ok := keysym2HID[k]
	return hid, ok
}
