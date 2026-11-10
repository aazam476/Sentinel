// SPDX-License-Identifier: Apache-2.0

package x11

// evdevOffset turns a Linux input event code into an X keycode, as the
// evdev keyboard driver (the default everywhere, Xvfb included) numbers
// them.
const evdevOffset = 8

// keycode returns the X keycode of the key a browser names by its
// KeyboardEvent.code. The code names the physical key, as does a Linux
// input event code, so the key typed is whatever the machine's own layout
// puts there.
func keycode(code string) (byte, bool) {
	scancode, ok := scancodes()[code]
	if !ok {
		return 0, false
	}
	return byte(scancode + evdevOffset), true //nolint:gosec // G115: the table's codes are all under 248
}

// scancodes are Linux input event codes (linux/input-event-codes.h) by
// the KeyboardEvent.code of the same key. It is a function, not a
// package variable, so nothing can change it.
func scancodes() map[string]int {
	codes := map[string]int{
		"Escape": 1, "Minus": 12, "Equal": 13, "Backspace": 14, "Tab": 15,
		"BracketLeft": 26, "BracketRight": 27, "Enter": 28, "ControlLeft": 29,
		"Semicolon": 39, "Quote": 40, "Backquote": 41, "ShiftLeft": 42, "Backslash": 43,
		"Comma": 51, "Period": 52, "Slash": 53, "ShiftRight": 54, "NumpadMultiply": 55,
		"AltLeft": 56, "Space": 57, "CapsLock": 58, "NumLock": 69, "ScrollLock": 70,
		"Numpad7": 71, "Numpad8": 72, "Numpad9": 73, "NumpadSubtract": 74, "Numpad4": 75,
		"Numpad5": 76, "Numpad6": 77, "NumpadAdd": 78, "Numpad1": 79, "Numpad2": 80,
		"Numpad3": 81, "Numpad0": 82, "NumpadDecimal": 83, "IntlBackslash": 86,
		"F11": 87, "F12": 88, "NumpadEnter": 96, "ControlRight": 97, "NumpadDivide": 98,
		"PrintScreen": 99, "AltRight": 100, "Home": 102, "ArrowUp": 103, "PageUp": 104,
		"ArrowLeft": 105, "ArrowRight": 106, "End": 107, "ArrowDown": 108, "PageDown": 109,
		"Insert": 110, "Delete": 111, "Pause": 119, "MetaLeft": 125, "MetaRight": 126,
		"ContextMenu": 127,
	}
	for i, key := range "1234567890" {
		codes["Digit"+string(key)] = 2 + i
	}
	for code, rows := range map[int]string{16: "QWERTYUIOP", 30: "ASDFGHJKL", 44: "ZXCVBNM"} {
		for i, key := range rows {
			codes["Key"+string(key)] = code + i
		}
	}
	for i := range 9 {
		codes["F"+string(rune('1'+i))] = 59 + i // F1 to F9
	}
	codes["F10"] = 68
	return codes
}
