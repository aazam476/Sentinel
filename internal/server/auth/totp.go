// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"image/png"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"
)

// TOTP settings. These are the defaults every authenticator app supports.
const (
	totpIssuer    = "Sentinel"
	totpPeriod    = 30 * time.Second
	totpDigits    = otp.DigitsSix
	totpQRSize    = 256
	totpSecretLen = 20 // bytes, the size RFC 4226 recommends for SHA-1
)

// TOTPEnrollment is what a user needs to add Sentinel to an authenticator
// app.
type TOTPEnrollment struct {
	// Secret is the base32 secret, for typing in by hand.
	Secret string
	// URL is the otpauth:// URL that the QR code encodes.
	URL string
	// QRCode is a PNG image of URL as a data: URL, ready for an <img>.
	QRCode string
}

// newTOTPEnrollment creates a new random TOTP secret for username.
func newTOTPEnrollment(username string) (TOTPEnrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: username,
		Period:      uint(totpPeriod / time.Second),
		SecretSize:  totpSecretLen,
		Digits:      totpDigits,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return TOTPEnrollment{}, fmt.Errorf("generate TOTP secret: %w", err)
	}

	image, err := key.Image(totpQRSize, totpQRSize)
	if err != nil {
		return TOTPEnrollment{}, fmt.Errorf("draw TOTP QR code: %w", err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image); err != nil {
		return TOTPEnrollment{}, fmt.Errorf("encode TOTP QR code: %w", err)
	}

	return TOTPEnrollment{
		Secret: key.Secret(),
		URL:    key.URL(),
		QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()),
	}, nil
}

// verifyTOTP checks code against secret at time now. It accepts the current
// 30-second step and one step either side, to allow for clocks that are a
// little off. It returns the step that matched.
//
// A code is accepted at most once: steps at or before lastUsedStep are
// skipped, so a code someone saw over the user's shoulder can't be replayed
// within its validity window.
func verifyTOTP(secret, code string, now time.Time, lastUsedStep int64) (step int64, ok bool) {
	if len(code) != 6 {
		return 0, false
	}

	currentStep := now.Unix() / int64(totpPeriod/time.Second)
	for _, candidate := range []int64{currentStep - 1, currentStep, currentStep + 1} {
		if candidate <= lastUsedStep || candidate < 0 {
			continue
		}
		expected, err := hotp.GenerateCodeCustom(secret, uint64(candidate), hotp.ValidateOpts{
			Digits:    totpDigits,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return candidate, true
		}
	}
	return 0, false
}
