// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"crypto/rand"
	"time"
)

// IDs are ULIDs: 26 characters of Crockford base32, the first 10 encoding
// the creation time in milliseconds and the rest 80 random bits. They sort
// by time, which makes logs easy to follow, and need no coordination.
// https://github.com/ulid/spec

// crockford is the base32 alphabet ULIDs use: no I, L, O or U, so IDs can't
// be misread or spell words.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const idLength = 26

// NewID returns a new ULID for a message created at now.
func NewID(now time.Time) string {
	var value [16]byte
	milliseconds := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		value[i] = byte(milliseconds)
		milliseconds >>= 8
	}
	// crypto/rand.Read never returns an error: if the system's random source
	// fails, Go stops the program rather than hand out predictable bytes.
	_, _ = rand.Read(value[6:])
	return encodeULID(value)
}

// encodeULID writes 128 bits as 26 base32 characters (130 bits, the top two
// always zero), five bits at a time from the most significant end.
func encodeULID(value [16]byte) string {
	id := make([]byte, idLength)
	for i := range idLength {
		// Bit position of this character's lowest bit, counted from the
		// least significant end of the 130-bit number.
		shift := uint(5 * (idLength - 1 - i))
		id[i] = crockford[bitsAt(value, shift)]
	}
	return string(id)
}

// bitsAt returns the 5 bits of value starting at bit shift (0 = least
// significant bit of the last byte).
func bitsAt(value [16]byte, shift uint) byte {
	var result byte
	for bit := range uint(5) {
		position := shift + bit
		if position >= 128 {
			continue
		}
		byteIndex := 15 - position/8
		if value[byteIndex]&(1<<(position%8)) != 0 {
			result |= 1 << bit
		}
	}
	return result
}

// IsID reports whether id looks like a ULID.
func IsID(id string) bool {
	if len(id) != idLength || id[0] > '7' {
		return false
	}
	for i := range len(id) {
		if !isCrockford(id[i]) {
			return false
		}
	}
	return true
}

func isCrockford(character byte) bool {
	for i := range len(crockford) {
		if crockford[i] == character {
			return true
		}
	}
	return false
}
