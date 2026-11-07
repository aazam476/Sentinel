// SPDX-License-Identifier: Apache-2.0

// Package config reads and checks the server's configuration file,
// server.toml.
//
// cmd/sentinel-server calls Load to read the file, applies any command-line
// overrides to the returned File, and calls Resolve to validate everything
// and turn it into a Config with parsed values (URLs, CIDR ranges). The rest
// of the server only ever sees a Config, so it never has to re-check a
// setting.
//
// Resolve is where the transport security rules live: TLS is on by default,
// and plain HTTP is only allowed on a loopback address or when the operator
// explicitly says a TLS-terminating proxy sits in front (SEC-10).
package config
