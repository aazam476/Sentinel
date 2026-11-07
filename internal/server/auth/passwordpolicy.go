// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bufio"
	_ "embed" // for the //go:embed directive below
	"errors"
	"strings"
	"unicode/utf8"
)

// Password length limits. The minimum comes from SEC-02. The maximum stops
// someone from making the server hash megabytes of input.
const (
	MinPasswordLength = 12
	MaxPasswordLength = 256
)

// commonPasswordList holds 46,886 widely used passwords of 12 or more
// characters, lowercased. Shorter ones fail the length rule anyway. Source
// and license: commonpasswords/README.md.
//
//go:embed commonpasswords/list.txt
var commonPasswordList string

// Password policy errors. They are never reassigned; the API shows their
// text to the user.
var (
	ErrPasswordTooShort       = errors.New("the password must be at least 12 characters")
	ErrPasswordTooLong        = errors.New("the password must be at most 256 characters")
	ErrPasswordCommon         = errors.New("this password is on a list of commonly used passwords; choose another")
	ErrPasswordSameAsUsername = errors.New("the password must not be the same as the username")
)

// passwordPolicy checks new passwords.
type passwordPolicy struct {
	common map[string]struct{}
}

func newPasswordPolicy() *passwordPolicy {
	common := make(map[string]struct{}, 50_000)
	scanner := bufio.NewScanner(strings.NewReader(commonPasswordList))
	for scanner.Scan() {
		common[scanner.Text()] = struct{}{}
	}
	return &passwordPolicy{common: common}
}

// Check returns nil if password is acceptable for username, or an error
// saying what is wrong with it.
func (p *passwordPolicy) Check(username, password string) error {
	length := utf8.RuneCountInString(password)
	if length < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if length > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	if strings.EqualFold(password, username) {
		return ErrPasswordSameAsUsername
	}
	if _, found := p.common[strings.ToLower(password)]; found {
		return ErrPasswordCommon
	}
	return nil
}
