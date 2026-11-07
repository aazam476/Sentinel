// SPDX-License-Identifier: Apache-2.0

package web

import (
	"path"
	"strings"
)

// contentTypeForPath returns the Content-Type for a file the web build
// produces, chosen by its extension, or "" for an extension not in the list
// (the file server then guesses as usual).
//
// Go's mime package adds types from the operating system (the Windows
// registry, /etc/mime.types on Linux), so the same binary would otherwise
// send different headers on different hosts. Windows, for example, often
// reports JavaScript as "application/javascript". A fixed list keeps the
// server's behavior identical everywhere.
// https://pkg.go.dev/mime#TypeByExtension
func contentTypeForPath(filePath string) string {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json"
	case ".webmanifest":
		return "application/manifest+json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".txt":
		return "text/plain; charset=utf-8"
	default:
		return ""
	}
}
