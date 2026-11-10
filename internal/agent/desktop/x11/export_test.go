// SPDX-License-Identifier: Apache-2.0

package x11

import (
	"image"

	"github.com/jezek/xgb"
)

// PromptLayout exposes promptLayout to tests.
func PromptLayout(screenWidth int) (frame, allow, deny image.Rectangle) {
	return promptLayout(screenWidth)
}

// IndicatorLayout exposes indicatorLayout to tests.
func IndicatorLayout(screenWidth int) image.Rectangle { return indicatorLayout(screenWidth) }

// Wrap exposes wrap to tests.
func Wrap(text string, width, limit int) []string { return wrap(text, width, limit) }

// RegisterExtensions lets a test's own connection use the extensions the
// package's connections use, safely (see registerExtensions).
func RegisterExtensions(conn *xgb.Conn) { registerExtensions(conn) }
