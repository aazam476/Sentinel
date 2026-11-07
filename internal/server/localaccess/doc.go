// SPDX-License-Identifier: Apache-2.0

// Package localaccess checks that a command which changes the server's data
// from the command line is run by someone allowed to: on Unix, root or the
// user that owns the data directory (normally "sentinel"); on Windows, an
// account that can write the database file, which the data directory's
// access list limits to SYSTEM and Administrators.
//
// cmd/sentinel-server calls Check before reset-admin (SEC-08), so a local
// user who merely can run the binary can't take over the admin account.
package localaccess
