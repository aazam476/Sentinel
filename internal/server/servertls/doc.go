// SPDX-License-Identifier: Apache-2.0

// Package servertls provides the TLS settings the server uses for HTTPS
// (SEC-10).
//
// cmd/sentinel-server picks one of two constructors based on the configured
// TLS mode:
//
//   - SelfSigned makes sure a self-signed certificate exists in
//     <data dir>/tls/, creating or renewing it when needed, and serves it.
//     Browsers show a warning the first time, which the operator accepts once.
//   - ACME obtains and renews a certificate automatically from Let's Encrypt
//     for the public URL's host name, caching it in <data dir>/tls/acme/.
//
// Both return a *tls.Config that only allows TLS 1.2 and newer.
package servertls
