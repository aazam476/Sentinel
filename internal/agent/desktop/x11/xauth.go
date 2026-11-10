// SPDX-License-Identifier: Apache-2.0

package x11

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
)

// Xauthority address families.
const (
	familyLocal = 256
	familyWild  = 65535
)

// cookieName is the only authorization X servers use in practice.
const cookieName = "MIT-MAGIC-COOKIE-1"

// ErrNoCookie means an Xauthority file holds no cookie for the display.
var ErrNoCookie = errors.New("no cookie for this display in the Xauthority file")

// errTruncated means an Xauthority file ends in the middle of an entry.
var errTruncated = errors.New("the Xauthority file is truncated")

// authEntry is one entry of an Xauthority file.
type authEntry struct {
	family uint16
	number string
	name   string
	data   []byte
}

// CookieFor returns, as hex, the MIT-MAGIC-COOKIE-1 an Xauthority file
// holds for a local display. The file is a series of entries, each a
// big-endian family then four length-prefixed fields: address, display
// number, authorization name and data.
func CookieFor(file []byte, display int) (string, error) {
	want := strconv.Itoa(display)
	for len(file) > 0 {
		entry, rest, err := readEntry(file)
		if err != nil {
			return "", err
		}
		file = rest
		local := entry.family == familyLocal || entry.family == familyWild
		if local && (entry.number == want || entry.number == "") && entry.name == cookieName && len(entry.data) == 16 {
			return hex.EncodeToString(entry.data), nil
		}
	}
	return "", ErrNoCookie
}

func readEntry(file []byte) (authEntry, []byte, error) {
	if len(file) < 2 {
		return authEntry{}, nil, errTruncated
	}
	entry := authEntry{family: binary.BigEndian.Uint16(file)}
	file = file[2:]
	var fields [4][]byte
	for i := range fields {
		field, rest, err := readField(file)
		if err != nil {
			return authEntry{}, nil, err
		}
		fields[i], file = field, rest
	}
	entry.number, entry.name, entry.data = string(fields[1]), string(fields[2]), fields[3]
	return entry, file, nil
}

func readField(file []byte) ([]byte, []byte, error) {
	if len(file) < 2 {
		return nil, nil, errTruncated
	}
	length := int(binary.BigEndian.Uint16(file))
	if len(file) < 2+length {
		return nil, nil, errTruncated
	}
	return file[2 : 2+length], file[2+length:], nil
}
