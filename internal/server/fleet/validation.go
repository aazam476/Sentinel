// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// Limits on what admins and agents can store.
const (
	maxDisplayNameLength = 64
	maxNoteLength        = 100
	maxTagLength         = 32
	maxTagsPerAgent      = 20
	// maxHostnameLength is the longest DNS name.
	maxHostnameLength = 253
	// maxReportedLength caps other text an agent reports (OS, version, ...).
	maxReportedLength = 128
)

// cleanText trims text and checks it is at most maxLength characters with
// no control characters, which could break the UI's layout, and no
// bidirectional controls, which can make text read differently from what
// it is (a machine named "nas" that displays as "san").
func cleanText(field, text string, maxLength int) (string, error) {
	text = strings.TrimSpace(text)
	if !utf8.ValidString(text) || auth.HasHiddenControls(text) {
		return "", &auth.ValidationError{Field: field, Message: "This can't contain control characters."}
	}
	if utf8.RuneCountInString(text) > maxLength {
		return "", &auth.ValidationError{Field: field, Message: "This is too long."}
	}
	return text, nil
}

// requiredText is cleanText for a value that can't be empty.
func requiredText(field, text string, maxLength int) (string, error) {
	text, err := cleanText(field, text, maxLength)
	if err == nil && text == "" {
		return "", &auth.ValidationError{Field: field, Message: "This is required."}
	}
	return text, err
}

// cleanTags lowercases, de-duplicates and sorts tags, and checks each is a
// short word made of letters, digits, dots, dashes and underscores.
func cleanTags(tags []string) ([]string, error) {
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if !isValidTag(tag) {
			return nil, &auth.ValidationError{Field: "tags",
				Message: "Tags are 1 to 32 letters, digits, dots, dashes or underscores, starting with a letter or digit."}
		}
		cleaned = append(cleaned, tag)
	}
	slices.Sort(cleaned)
	cleaned = slices.Compact(cleaned)
	if len(cleaned) > maxTagsPerAgent {
		return nil, &auth.ValidationError{Field: "tags", Message: "A machine can have at most 20 tags."}
	}
	return cleaned, nil
}

func isValidTag(tag string) bool {
	if tag == "" || len(tag) > maxTagLength {
		return false
	}
	for i, r := range tag {
		letterOrDigit := unicode.IsLetter(r) || unicode.IsDigit(r)
		if i == 0 && !letterOrDigit {
			return false
		}
		if !letterOrDigit && r != '.' && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
