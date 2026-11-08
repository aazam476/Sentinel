// SPDX-License-Identifier: Apache-2.0

// Package softauthn is a passkey authenticator in software, for tests. It
// answers the server's navigator.credentials.create() and .get() options
// with real ECDSA P-256 signatures, the way a browser and a platform
// authenticator would, so passkey sign-in can be tested end to end without
// a browser.
//
// It only implements what Sentinel's tests need: "none" attestation, one
// credential per authenticator, and user presence and verification always
// granted.
package softauthn
