// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/settings"
)

// SettingsResponse is the Settings page (§9.10): what admins change here,
// and what server.toml sets, shown read-only.
type SettingsResponse struct {
	Retention MetricsRetention `json:"retention"`
	// UpdateChannel is "stable" or "beta": where agent builds come from.
	UpdateChannel string       `json:"updateChannel"`
	Server        ServerConfig `json:"server"`
}

// SettingsRequest changes the settings admins can change here.
type SettingsRequest struct {
	Retention     MetricsRetention `json:"retention"`
	UpdateChannel string           `json:"updateChannel"`
}

// MetricsRetention is how long metrics history is kept (§9.1).
type MetricsRetention struct {
	RawHours   int `json:"rawHours"`
	MinuteDays int `json:"minuteDays"`
	HourDays   int `json:"hourDays"`
}

// ServerConfig is server.toml as the server resolved it.
type ServerConfig struct {
	Listen         string   `json:"listen"`
	PublicURL      string   `json:"publicUrl"`
	BasePath       string   `json:"basePath"`
	TrustedProxies []string `json:"trustedProxies"`
	TLSMode        string   `json:"tlsMode"`
	ACMEEmail      string   `json:"acmeEmail"`
	BehindTLSProxy bool     `json:"behindTlsProxy"`
	ReleaseURL     string   `json:"releaseUrl"`
	MaxFileBytes   int64    `json:"maxFileBytes"`
	TURNListen     string   `json:"turnListen"`
}

func serverConfig(resolved config.Config) ServerConfig {
	view := ServerConfig{
		Listen: resolved.ListenAddress, BasePath: resolved.BasePath, TrustedProxies: []string{},
		TLSMode: string(resolved.TLS.Mode), ACMEEmail: resolved.TLS.ACMEEmail, BehindTLSProxy: resolved.TLS.BehindTLSProxy,
		ReleaseURL: resolved.ReleaseURL, MaxFileBytes: resolved.MaxFileBytes, TURNListen: resolved.TURN.Listen,
	}
	if resolved.PublicURL != nil {
		view.PublicURL = resolved.PublicURL.String()
	}
	for _, proxy := range resolved.TrustedProxies {
		view.TrustedProxies = append(view.TrustedProxies, proxy.String())
	}
	return view
}

// handleGetSettings is GET /api/settings, for admins.
func (h *handlers) handleGetSettings(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	current, err := h.settings.Get(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.settingsResponse(current))
}

// handleUpdateSettings is PUT /api/settings, after a recent step-up.
func (h *handlers) handleUpdateSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request SettingsRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	saved, err := h.settings.Update(r.Context(), session, settings.Settings{Retention: settings.Retention(request.Retention), UpdateChannel: request.UpdateChannel}, requestInfo(r))
	var invalid *settings.InvalidError
	if errors.As(err, &invalid) {
		writeInvalidInput(w, invalid.Field, invalid.Reason)
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.settingsResponse(saved))
}

func (h *handlers) settingsResponse(current settings.Settings) SettingsResponse {
	return SettingsResponse{Retention: MetricsRetention(current.Retention), UpdateChannel: current.UpdateChannel, Server: serverConfig(h.config)}
}
