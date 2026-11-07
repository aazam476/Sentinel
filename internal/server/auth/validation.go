// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"regexp"
	"strings"
)

// usernamePattern allows letters, digits, dots, dashes and underscores,
// starting with a letter or digit. Plain names avoid look-alike characters
// and anything that needs escaping in logs or URLs.
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// validateUsername returns the trimmed username, or a ValidationError.
func validateUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if !usernamePattern.MatchString(username) {
		return "", &ValidationError{
			Field:   "username",
			Message: "use 1 to 64 letters, digits, dots, dashes or underscores, starting with a letter or digit",
		}
	}
	return username, nil
}

// validatePassword checks a new password against the policy (SEC-02).
func (s *Service) validatePassword(username, password string) error {
	if err := s.policy.Check(username, password); err != nil {
		return &ValidationError{Field: "password", Message: err.Error()}
	}
	return nil
}

// validateRole checks a role name.
func validateRole(role Role) error {
	if !role.IsValid() {
		return &ValidationError{Field: "role", Message: "choose viewer, operator or admin"}
	}
	return nil
}
