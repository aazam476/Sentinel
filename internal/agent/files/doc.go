// SPDX-License-Identifier: Apache-2.0

// Package files is the agent's side of the file manager (§9.7): listing
// folders, reading files in chunks, uploads that can resume, folders,
// renames, deletes, and editing small text files.
//
// Every path from the server is checked before use: it must be absolute,
// and when the local policy limits the file manager to some folders
// (SEC-25), the path is resolved through any symbolic links first and must
// then lie inside one of them, so a link can't lead outside.
package files
