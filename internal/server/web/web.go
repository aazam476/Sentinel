// SPDX-License-Identifier: Apache-2.0

package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// builtFiles holds the Vite build output. The "all:" prefix includes
// dist/.gitkeep, the placeholder that keeps the folder in git, so the embed
// still compiles before the UI has been built.
//
//go:embed all:dist
var builtFiles embed.FS

// notBuiltMessage is the response body when the binary was built without the
// web UI.
const notBuiltMessage = "The Sentinel web UI is not part of this build. " +
	"Run `make build` (or `make web` and then `go build`) to include it.\n"

// Handler returns an http.Handler that serves the web UI embedded in this
// binary.
func Handler() (http.Handler, error) {
	files, err := fs.Sub(builtFiles, "dist")
	if err != nil {
		return nil, fmt.Errorf("open embedded web UI: %w", err)
	}
	return NewHandler(files), nil
}

// NewHandler returns an http.Handler that serves the web UI from files, which
// must contain index.html at its root. Tests pass an in-memory file system.
//
// If files has no index.html, every request gets 503 Service Unavailable with
// a message explaining how to build the UI.
func NewHandler(files fs.FS) http.Handler {
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return http.HandlerFunc(serveNotBuilt)
	}

	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The file server lists the contents of any folder without an
		// index.html. Nobody needs a listing of the build output, so folder
		// paths other than the root are treated as missing.
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		// The file server keeps a Content-Type that is already set.
		if contentType := contentTypeForPath(r.URL.Path); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveNotBuilt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	// The status code already tells the client what happened; if writing the
	// explanation fails there is nobody left to report it to.
	_, _ = fmt.Fprint(w, notBuiltMessage)
}
