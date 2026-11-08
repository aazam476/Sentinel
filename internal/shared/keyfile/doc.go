// SPDX-License-Identifier: Apache-2.0

// Package keyfile reads and creates small secret files, such as private
// keys, that only their owner may read. Both binaries keep keys this way:
// the server its secrets key and identity key, the agent its identity key.
//
// On Unix a key file must have mode 0600 or stricter; a file that other
// users can read is refused with a message saying how to fix it. Windows
// has no such modes: there the file lives in a directory whose access list
// already limits who can read it.
package keyfile
