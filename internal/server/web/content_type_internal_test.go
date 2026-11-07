// SPDX-License-Identifier: Apache-2.0

package web

import "testing"

func TestContentTypeForPath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{name: "html is utf-8 text/html", filePath: "/index.html", want: "text/html; charset=utf-8"},
		{name: "javascript is text/javascript on every OS", filePath: "/assets/index-abc.js", want: "text/javascript; charset=utf-8"},
		{name: "javascript modules are text/javascript", filePath: "/assets/worker.mjs", want: "text/javascript; charset=utf-8"},
		{name: "css is text/css", filePath: "/assets/index.css", want: "text/css; charset=utf-8"},
		{name: "json is application/json", filePath: "/data.json", want: "application/json"},
		{name: "source maps are json", filePath: "/assets/index.js.map", want: "application/json"},
		{name: "web manifests have their own type", filePath: "/site.webmanifest", want: "application/manifest+json"},
		{name: "svg is image/svg+xml", filePath: "/icon.svg", want: "image/svg+xml"},
		{name: "png is image/png", filePath: "/icon.png", want: "image/png"},
		{name: "ico is image/x-icon", filePath: "/favicon.ico", want: "image/x-icon"},
		{name: "woff2 is font/woff2", filePath: "/fonts/inter.woff2", want: "font/woff2"},
		{name: "txt is utf-8 text/plain", filePath: "/robots.txt", want: "text/plain; charset=utf-8"},
		{name: "extensions are matched in any letter case", filePath: "/ASSETS/INDEX.JS", want: "text/javascript; charset=utf-8"},
		{name: "unknown extensions are left to the file server", filePath: "/archive.xyz", want: ""},
		{name: "paths without an extension are left to the file server", filePath: "/", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := contentTypeForPath(test.filePath); got != test.want {
				t.Errorf("contentTypeForPath(%q) = %q, want %q", test.filePath, got, test.want)
			}
		})
	}
}
