// SPDX-License-Identifier: Apache-2.0

package middleware

import "net/http"

// contentSecurityPolicy only lets the page load scripts, styles, fonts and
// data from the server itself. There are no inline scripts and no third-party
// origins. "data:" images allow the TOTP QR code, which the server sends as a
// data URL. base-uri 'self' allows the <base> element the server adds for the
// base path.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// SecurityHeadersOptions configures SecurityHeaders.
type SecurityHeadersOptions struct {
	// StrictTransportSecurity adds an HSTS header. Only turn this on with a
	// certificate browsers trust: once a browser has seen HSTS it refuses to
	// let the user click through a certificate warning, so a self-signed
	// certificate would lock everyone out.
	StrictTransportSecurity bool
}

// SecurityHeaders adds the browser security headers (SEC-11) to every
// response.
func SecurityHeaders(next http.Handler, options SecurityHeadersOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Content-Security-Policy", contentSecurityPolicy)
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("Cross-Origin-Opener-Policy", "same-origin")
		headers.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if options.StrictTransportSecurity {
			headers.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
