// SPDX-License-Identifier: Apache-2.0

//go:build windows

package windesktop

// scancode returns the scancode (set 1, as Windows takes it) of the key a
// browser names by its KeyboardEvent.code, and whether it is an extended
// key. Scancodes name the physical key, so the machine's layout decides
// what it types.
func scancode(code string) (uint16, bool, bool) {
	if scan, ok := extendedScancodes()[code]; ok {
		return scan, true, true
	}
	scan, ok := baseScancodes()[code]
	return scan, false, ok
}

// baseScancodes are the keys without the E0 prefix.
func baseScancodes() map[string]uint16 {
	codes := map[string]uint16{
		"Escape": 0x01, "Minus": 0x0C, "Equal": 0x0D, "Backspace": 0x0E, "Tab": 0x0F,
		"BracketLeft": 0x1A, "BracketRight": 0x1B, "Enter": 0x1C, "ControlLeft": 0x1D,
		"Semicolon": 0x27, "Quote": 0x28, "Backquote": 0x29, "ShiftLeft": 0x2A, "Backslash": 0x2B,
		"Comma": 0x33, "Period": 0x34, "Slash": 0x35, "ShiftRight": 0x36, "NumpadMultiply": 0x37,
		"AltLeft": 0x38, "Space": 0x39, "CapsLock": 0x3A, "NumLock": 0x45, "ScrollLock": 0x46,
		"Numpad7": 0x47, "Numpad8": 0x48, "Numpad9": 0x49, "NumpadSubtract": 0x4A, "Numpad4": 0x4B,
		"Numpad5": 0x4C, "Numpad6": 0x4D, "NumpadAdd": 0x4E, "Numpad1": 0x4F, "Numpad2": 0x50,
		"Numpad3": 0x51, "Numpad0": 0x52, "NumpadDecimal": 0x53, "IntlBackslash": 0x56,
		"F11": 0x57, "F12": 0x58, "Pause": 0x45,
	}
	for i, key := range "1234567890" {
		codes["Digit"+string(key)] = uint16(0x02 + i) //nolint:gosec // G115: small
	}
	for start, row := range map[uint16]string{0x10: "QWERTYUIOP", 0x1E: "ASDFGHJKL", 0x2C: "ZXCVBNM"} {
		for i, key := range row {
			codes["Key"+string(key)] = start + uint16(i) //nolint:gosec // G115: small
		}
	}
	for i := range 10 {
		codes["F"+string(rune('1'+i))] = uint16(0x3B + i)
	}
	delete(codes, "F:") // the loop above names F10 "F:"
	codes["F10"] = 0x44
	return codes
}

// extendedScancodes are the keys sent with the E0 prefix.
func extendedScancodes() map[string]uint16 {
	return map[string]uint16{
		"NumpadEnter": 0x1C, "ControlRight": 0x1D, "NumpadDivide": 0x35, "PrintScreen": 0x37,
		"AltRight": 0x38, "Home": 0x47, "ArrowUp": 0x48, "PageUp": 0x49, "ArrowLeft": 0x4B,
		"ArrowRight": 0x4D, "End": 0x4F, "ArrowDown": 0x50, "PageDown": 0x51, "Insert": 0x52,
		"Delete": 0x53, "MetaLeft": 0x5B, "MetaRight": 0x5C, "ContextMenu": 0x5D,
	}
}
