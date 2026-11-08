// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/registry"
)

// errorCodeRegistryExists: the registry already has a login.
const errorCodeRegistryExists = "registry_exists"

// RegistryLogin is a stored registry login; its password is never sent
// back.
type RegistryLogin struct {
	ID            int64  `json:"id"`
	ServerAddress string `json:"serverAddress"`
	Username      string `json:"username"`
	CreatedAt     int64  `json:"createdAt"`
}

// RegistryLogins answers GET /api/registries.
type RegistryLogins struct {
	Logins []RegistryLogin `json:"logins"`
}

// AddRegistryLoginRequest is the body of POST /api/registries.
type AddRegistryLoginRequest struct {
	ServerAddress string `json:"serverAddress"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

func (h *handlers) handleListRegistryLogins(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	logins, err := h.registries.List(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := RegistryLogins{Logins: make([]RegistryLogin, 0, len(logins))}
	for _, login := range logins {
		response.Logins = append(response.Logins, RegistryLogin(login))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleAddRegistryLogin(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request AddRegistryLoginRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	login, err := h.registries.Add(r.Context(), session, requestInfo(r), registry.LoginInput(request))
	if errors.Is(err, registry.ErrExists) {
		writeError(w, http.StatusConflict, errorCodeRegistryExists, "That registry already has a login. Remove it first to change it.")
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, RegistryLogin(login))
}

func (h *handlers) handleRemoveRegistryLogin(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, err := strconv.ParseInt(r.PathValue("loginId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "There is no registry login with that ID.")
		return
	}
	err = h.registries.Remove(r.Context(), session, requestInfo(r), id)
	if errors.Is(err, registry.ErrNotFound) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "There is no registry login with that ID.")
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
