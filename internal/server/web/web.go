// SPDX-License-Identifier: Apache-2.0

package web

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"path"
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
// binary. basePath is the configured base path, for example "/" or
// "/sentinel".
func Handler(basePath string) (http.Handler, error) {
	files, err := fs.Sub(builtFiles, "dist")
	if err != nil {
		return nil, fmt.Errorf("open embedded web UI: %w", err)
	}
	return NewHandler(files, basePath), nil
}

// NewHandler returns an http.Handler that serves the web UI from files, which
// must contain index.html at its root. Tests pass an in-memory file system.
//
// Files that exist are served as they are. Any other path without a file
// extension, such as /login or /machines/42, is a page of the single-page
// app, so it gets index.html and the app's router picks the page. index.html
// is given a <base> element for basePath, so the app's relative asset links
// work under any base path.
//
// If files has no index.html, every request gets 503 Service Unavailable with
// a message explaining how to build the UI.
func NewHandler(files fs.FS, basePath string) http.Handler {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return http.HandlerFunc(serveNotBuilt)
	}

	return &singlePageApp{
		files:      files,
		fileServer: http.FileServerFS(files),
		index:      withBaseElement(index, basePath),
	}
}

// singlePageApp serves the built web UI.
type singlePageApp struct {
	files      fs.FS
	fileServer http.Handler
	index      []byte
}

func (app *singlePageApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// The file server lists folder contents; nobody needs a listing of the
	// build output, so folder paths other than the root are missing.
	if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	switch {
	case name == "" || name == "index.html":
		app.serveIndex(w)
	case isFile(app.files, name):
		app.serveFile(w, r, name)
	case path.Ext(name) == "":
		app.serveIndex(w)
	default:
		http.NotFound(w, r)
	}
}

func (app *singlePageApp) serveIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// index.html names the current asset files, so it must be fetched fresh
	// after an upgrade.
	w.Header().Set("Cache-Control", "no-cache")
	// If the write fails the client has gone away; there is nobody to tell.
	_, _ = w.Write(app.index)
}

func (app *singlePageApp) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	// The file server keeps a Content-Type that is already set.
	if contentType := contentTypeForPath(name); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	// Vite puts a content hash in every file name under assets/, so a
	// changed file always gets a new name and old ones can be cached forever.
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	app.fileServer.ServeHTTP(w, r)
}

func isFile(files fs.FS, name string) bool {
	info, err := fs.Stat(files, name)
	return err == nil && !info.IsDir()
}

// withBaseElement inserts <base href="..."> right after <head>, so relative
// URLs in the page resolve against the base path instead of the current
// page's path.
func withBaseElement(index []byte, basePath string) []byte {
	href := basePath
	if !strings.HasSuffix(href, "/") {
		href += "/"
	}
	baseElement := []byte(`<head><base href="` + html.EscapeString(href) + `" />`)
	return bytes.Replace(index, []byte("<head>"), baseElement, 1)
}

func serveNotBuilt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	// The status code already tells the client what happened; if writing the
	// explanation fails there is nobody left to report it to.
	_, _ = fmt.Fprint(w, notBuiltMessage)
}
