// SPDX-License-Identifier: Apache-2.0

// Package web serves the browser UI from inside the server binary.
//
// The React app in web/ is built by Vite, and `make web` copies the result
// into internal/server/web/dist. The Go compiler embeds that folder into the
// binary, so a release is a single file with no separate static assets to
// install.
//
// When the UI hasn't been built (for example a plain `go build` on a fresh
// checkout), the dist folder holds only a placeholder and the handler answers
// every request with a short message explaining how to build it.
// See docs/decisions/0003-embedding-the-web-ui.md.
package web
