# 0026 — Encrypted backups

**Date:** 2026-11-09 · **Status:** Accepted

## Context

§9.10 asks for `sentinel-server backup <file>` and `restore <file>`: the database, the secret
key and the config, encrypted with a passphrase. Without the secret key a database is useless
(stored credentials can't be decrypted), and without the server's identity key and TLS
certificate every agent would refuse the restored server, so a backup has to carry them, and so
it must be at least as well protected as the data directory itself.

## Decision

- **What goes in**: a consistent copy of the database, made with SQLite's `VACUUM INTO` so a
  backup can be taken while the server runs; every other file in the data directory (the secret
  key, the server key, `tls/` with the self-signed certificate and the ACME cache), except the
  database's journals, the setup token and temporary files; and the config file, if there is
  one. They go into a tar archive, gzip-compressed.
- **How it is sealed**: a key is derived from the passphrase (at least 12 characters) with
  Argon2id (3 passes, 64 MiB, a random salt), and the archive is encrypted with AES-256-GCM in
  64 KiB chunks (the STREAM construction): each chunk's nonce holds the file's random prefix,
  the chunk's number and whether it is the last, and the header (format version and Argon2
  settings) is authenticated with every chunk. A changed byte, a reordered, dropped or extra
  chunk, a file cut short or with something appended, and a wrong passphrase are all refused;
  the reader can't tell a wrong passphrase from tampering and says so. Argon2 settings outside
  sane bounds are refused before any work, so a crafted file can't make a restore use gigabytes.
- **The passphrase** is read from `--passphrase-file`, else `$SENTINEL_BACKUP_PASSPHRASE`, else
  the first line of standard input, never from the command line, where other users could see it.
- **Backup** runs only as root or the data directory's owner (like `reset-admin`, SEC-08),
  creates the file with mode 0600, and never overwrites one.
- **Restore** refuses a data directory that already has a database unless `--force` is given
  (with the server stopped). It decrypts the whole backup into a folder inside the data
  directory first, and only when every chunk has checked out moves the files into place, so a
  wrong passphrase or a damaged file changes nothing. Entry names are checked: no absolute paths,
  `..`, backslashes or links. An existing config file is kept unless `--force` is given.

## Consequences

- Losing the passphrase loses the backup; the command says so.
- Restoring onto another host keeps the server's identity, so agents reconnect to it, provided
  they can reach it at the same address.
