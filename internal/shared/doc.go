// SPDX-License-Identifier: Apache-2.0

// Package shared is the parent of the few small helper packages that both the
// server and the agent use, such as buildinfo. It holds no code itself.
//
// Keep this tree small. Code belongs here only when both binaries genuinely
// need it; anything used by just one side lives under internal/server or
// internal/agent instead.
package shared
