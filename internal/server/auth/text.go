// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"strings"
	"unicode"
)

// HasHiddenControls reports whether text contains control characters,
// which can break the UI's layout or a log line, or bidirectional controls,
// which make text display differently from what it is (a name stored as
// "nas" that shows as "san"). Free-text names users and agents choose are
// refused if it does.
func HasHiddenControls(text string) bool {
	return strings.ContainsFunc(text, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r)
	})
}
