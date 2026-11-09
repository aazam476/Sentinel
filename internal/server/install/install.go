// SPDX-License-Identifier: Apache-2.0

package install

import (
	"crypto/ed25519"
	_ "embed"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/aazam476/sentinel/internal/server/updates"
	"github.com/aazam476/sentinel/internal/shared/release"
)

//go:embed agent.sh
var agentShell string

//go:embed agent.ps1
var agentPowerShell string

// Builds finds hosted builds (internal/server/updates).
type Builds interface {
	Newest(osName, arch string) (updates.Build, string, error)
}

// Handler serves the scripts and builds under /install/.
type Handler struct {
	builds Builds
	keys   []ed25519.PublicKey
}

// New returns a Handler; keys nil means the compiled-in release keys.
func New(builds Builds, keys []ed25519.PublicKey) *Handler {
	if keys == nil {
		keys = release.TrustedKeys()
	}
	return &Handler{builds: builds, keys: keys}
}

// Routes adds the install routes.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /install/agent.sh", h.script(agentShell, "text/x-shellscript"))
	mux.HandleFunc("GET /install/agent.ps1", h.script(agentPowerShell, "text/plain"))
	mux.HandleFunc("GET /install/agent/{os}/{arch}/{file}", h.build)
}

// script serves a script with the trusted keys written into it.
func (h *Handler) script(template, contentType string) http.HandlerFunc {
	keys := make([]string, 0, len(h.keys))
	for _, key := range h.keys {
		keys = append(keys, release.FormatPublicKey(key))
	}
	body := strings.Replace(template, "@RELEASE_KEYS@", strings.Join(keys, " "), 1)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType+"; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(body))
	}
}

// build serves the newest build for a system ("sentinel-agent") or its
// signature ("sentinel-agent.sig").
func (h *Handler) build(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if file != "sentinel-agent" && file != "sentinel-agent.sig" {
		http.NotFound(w, r)
		return
	}
	_, path, err := h.builds.Newest(r.PathValue("os"), r.PathValue("arch"))
	if err != nil {
		http.Error(w, "This server has no agent build for "+r.PathValue("os")+"/"+r.PathValue("arch")+
			". An admin can fetch the latest release on its Updates page.", http.StatusNotFound)
		return
	}
	if file == "sentinel-agent.sig" {
		path += ".sig"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Clean(path)) //nolint:gosec // G703: the path comes from the hosted builds, not the request
}
