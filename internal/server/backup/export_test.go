// SPDX-License-Identifier: Apache-2.0

package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

// WriteArchiveForTest writes a backup holding exactly entry, as no real
// backup would, so tests can check Read refuses it.
func WriteArchiveForTest(t *testing.T, passphrase []byte, entry tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	encrypter, err := newEncrypter(&out, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(encrypter)
	archive := tar.NewWriter(compressed)
	if err := archive.WriteHeader(&entry); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := encrypter.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
