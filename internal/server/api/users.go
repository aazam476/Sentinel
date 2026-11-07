// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// UserResponse is a user account as admins see it.
type UserResponse struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"createdAt"`
	Disabled  bool   `json:"disabled"`
}

// UsersResponse lists every user.
type UsersResponse struct {
	Users []UserResponse `json:"users"`
}

// CreateUserRequest adds a user, who sets up 2FA at first sign-in.
type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UpdateUserRequest changes a user. Missing fields are left as they are.
type UpdateUserRequest struct {
	Role     *string `json:"role"`
	Disabled *bool   `json:"disabled"`
}

func (h *handlers) handleListUsers(w http.ResponseWriter, r *http.Request, session auth.Session) {
	users, err := h.auth.ListUsers(r.Context(), session)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := UsersResponse{Users: make([]UserResponse, 0, len(users))}
	for _, user := range users {
		response.Users = append(response.Users, userResponse(user))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleCreateUser(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request CreateUserRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	user, err := h.auth.CreateUser(r.Context(), session, auth.CreateUserInput{
		Username: request.Username, Password: request.Password, Role: auth.Role(request.Role),
	}, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, userResponse(user))
}

func (h *handlers) handleUpdateUser(w http.ResponseWriter, r *http.Request, session auth.Session) {
	userID, ok := pathID(w, r)
	if !ok {
		return
	}
	var request UpdateUserRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	input := auth.UpdateUserInput{Disabled: request.Disabled}
	if request.Role != nil {
		role := auth.Role(*request.Role)
		input.Role = &role
	}
	user, err := h.auth.UpdateUser(r.Context(), session, userID, input, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, userResponse(user))
}

func userResponse(user auth.User) UserResponse {
	return UserResponse{
		ID: user.ID, Username: user.Username, Role: string(user.Role), CreatedAt: user.CreatedAt,
		Disabled: user.DisabledAt != nil,
	}
}
