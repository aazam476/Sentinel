// SPDX-License-Identifier: Apache-2.0

package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// magic starts every backup file.
const magic = "SENTINEL BACKUP\n"

// version is the format's version.
const version = 1

// chunkSize is how much of the archive each sealed chunk holds.
const chunkSize = 64 << 10

const (
	saltSize        = 16
	noncePrefixSize = 7
	headerSize      = len(magic) + 1 + 4 + 4 + 1 + saltSize + noncePrefixSize
	tagSize         = 16
)

// MinPassphraseLength is the shortest passphrase a backup accepts.
const MinPassphraseLength = 12

// Key derivation settings for new backups: Argon2id with 64 MiB.
const (
	kdfTime    = 3
	kdfMemory  = 64 * 1024 // KiB
	kdfThreads = 4
)

// ErrWrongPassphrase means the passphrase is wrong or the file was changed.
var ErrWrongPassphrase = errors.New("the passphrase is wrong, or the backup was changed or damaged")

// ErrNotABackup means the file isn't a Sentinel backup, or is from a newer
// version.
var ErrNotABackup = errors.New("this isn't a Sentinel backup, or it was made by a newer version")

// header is the start of a backup file.
type header struct {
	time, memory uint32
	threads      uint8
	salt         [saltSize]byte
	noncePrefix  [noncePrefixSize]byte
}

func (h header) bytes() []byte {
	out := make([]byte, 0, headerSize)
	out = append(out, magic...)
	out = append(out, version)
	out = binary.BigEndian.AppendUint32(out, h.time)
	out = binary.BigEndian.AppendUint32(out, h.memory)
	out = append(out, h.threads)
	out = append(out, h.salt[:]...)
	return append(out, h.noncePrefix[:]...)
}

func parseHeader(raw []byte) (header, error) {
	if len(raw) != headerSize || !bytes.HasPrefix(raw, []byte(magic)) || raw[len(magic)] != version {
		return header{}, ErrNotABackup
	}
	rest := raw[len(magic)+1:]
	h := header{time: binary.BigEndian.Uint32(rest), memory: binary.BigEndian.Uint32(rest[4:]), threads: rest[8]}
	copy(h.salt[:], rest[9:])
	copy(h.noncePrefix[:], rest[9+saltSize:])
	// Settings a real backup never has would make opening a crafted file
	// take minutes or gigabytes.
	if h.time < 1 || h.time > 10 || h.memory < 8*1024 || h.memory > 1024*1024 || h.threads < 1 || h.threads > 16 {
		return header{}, ErrNotABackup
	}
	return h, nil
}

func (h header) aead(passphrase []byte) cipher.AEAD {
	key := argon2.IDKey(passphrase, h.salt[:], h.time, h.memory, h.threads, 32)
	// aes.NewCipher fails only for a key that isn't 16, 24 or 32 bytes, and
	// cipher.NewGCM only for a block size other than 16.
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	return aead
}

// nonce is the chunk's nonce: the file's prefix, the chunk's number, and
// whether it is the last.
func (h header) nonce(counter uint32, last bool) []byte {
	nonce := make([]byte, 0, 12)
	nonce = append(nonce, h.noncePrefix[:]...)
	nonce = binary.BigEndian.AppendUint32(nonce, counter)
	if last {
		return append(nonce, 1)
	}
	return append(nonce, 0)
}

// encrypter seals what is written to it, chunk by chunk.
type encrypter struct {
	out     io.Writer
	h       header
	ad      []byte
	aead    cipher.AEAD
	buffer  []byte
	counter uint32
}

// newEncrypter writes a header to out and returns a writer that encrypts
// to it. Close seals the last chunk.
func newEncrypter(out io.Writer, passphrase []byte) (*encrypter, error) {
	if len(passphrase) < MinPassphraseLength {
		return nil, fmt.Errorf("the passphrase must be at least %d characters", MinPassphraseLength)
	}
	h := header{time: kdfTime, memory: kdfMemory, threads: kdfThreads}
	// crypto/rand.Read never fails: it crashes the program rather than
	// return predictable bytes.
	_, _ = rand.Read(h.salt[:])
	_, _ = rand.Read(h.noncePrefix[:])
	ad := h.bytes()
	if _, err := out.Write(ad); err != nil {
		return nil, fmt.Errorf("write the backup: %w", err)
	}
	return &encrypter{out: out, h: h, ad: ad, aead: h.aead(passphrase), buffer: make([]byte, 0, chunkSize)}, nil
}

func (e *encrypter) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		if len(e.buffer) == chunkSize {
			if err := e.seal(false); err != nil {
				return written, err
			}
		}
		n := copy(e.buffer[len(e.buffer):chunkSize], data)
		e.buffer = e.buffer[:len(e.buffer)+n]
		data, written = data[n:], written+n
	}
	return written, nil
}

// Close seals what is left as the last chunk.
func (e *encrypter) Close() error {
	return e.seal(true)
}

func (e *encrypter) seal(last bool) error {
	if e.counter == ^uint32(0) {
		return errors.New("the backup is too big")
	}
	nonce := e.h.nonce(e.counter, last)
	e.counter++
	// The chunk's length, then the chunk, written together.
	sealed := binary.BigEndian.AppendUint32(nil, uint32(len(e.buffer)+tagSize)) //nolint:gosec // G115: at most chunkSize+tagSize
	sealed = e.aead.Seal(sealed, nonce, e.buffer, e.ad)
	e.buffer = e.buffer[:0]
	if _, err := e.out.Write(sealed); err != nil {
		return fmt.Errorf("write the backup: %w", err)
	}
	return nil
}

// decrypter reads a backup's archive, checking every chunk.
type decrypter struct {
	in      io.Reader
	h       header
	ad      []byte
	aead    cipher.AEAD
	plain   []byte
	counter uint32
	done    bool
}

// newDecrypter reads the header from in and returns a reader of the
// archive. A wrong passphrase shows on the first read.
func newDecrypter(in io.Reader, passphrase []byte) (*decrypter, error) {
	raw := make([]byte, headerSize)
	if _, err := io.ReadFull(in, raw); err != nil {
		return nil, ErrNotABackup
	}
	h, err := parseHeader(raw)
	if err != nil {
		return nil, err
	}
	return &decrypter{in: in, h: h, ad: raw, aead: h.aead(passphrase)}, nil
}

func (d *decrypter) Read(out []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.done {
			return 0, io.EOF
		}
		if err := d.next(); err != nil {
			return 0, err
		}
	}
	n := copy(out, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}

// next opens the next chunk. A file that ends before its last chunk, or
// goes on after it, is refused.
func (d *decrypter) next() error {
	var length [4]byte
	if _, err := io.ReadFull(d.in, length[:]); err != nil {
		return ErrWrongPassphrase // cut short
	}
	size := binary.BigEndian.Uint32(length[:])
	if size < tagSize || size > chunkSize+tagSize {
		return ErrWrongPassphrase
	}
	sealed := make([]byte, size)
	if _, err := io.ReadFull(d.in, sealed); err != nil {
		return ErrWrongPassphrase
	}
	plain, err := d.aead.Open(nil, d.h.nonce(d.counter, false), sealed, d.ad)
	if err != nil {
		if plain, err = d.aead.Open(nil, d.h.nonce(d.counter, true), sealed, d.ad); err != nil {
			return ErrWrongPassphrase
		}
		d.done = true
		var extra [1]byte
		if _, err := io.ReadFull(d.in, extra[:]); err == nil {
			return ErrWrongPassphrase // something after the last chunk
		}
	}
	d.counter++
	d.plain = plain
	return nil
}
