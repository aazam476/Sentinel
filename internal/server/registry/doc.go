// SPDX-License-Identifier: Apache-2.0

// Package registry keeps Docker registry logins, encrypted with the
// server's secret key (SEC-42), and the results of checking machines'
// images for updates (§9.4). A login leaves the server only inside the one
// request to an agent that needs it.
package registry
