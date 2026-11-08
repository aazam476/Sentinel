# 0023 — The file manager

**Date:** 2026-11-08 · **Status:** Accepted

## Context

M6 adds a file manager (§9.7): browse folders, download and upload files of up to 10 GB
(configurable), resumable uploads, create, rename and delete, and edit text files under 1 MB in
the browser, within the folders the local policy allows (SEC-25). The agent runs as root, so
the file manager reaches everything root can unless the policy says otherwise.

The protocol caps a control message at 1 MB, and the hub closes a stream whose reader falls
behind, so that one slow viewer can't stall an agent's whole connection. That suits a terminal
but not a 10 GB file going to a browser on a slow link.

## Decision

- **File data moves in chunks, one request each, of at most 512 KB**: `file.read` with an offset
  for downloads; `file.upload.begin`, `file.upload.write` at an offset, and
  `file.upload.finish` for uploads. A transfer can only go as fast as its slowest side, because
  the server asks for the next chunk only after the browser has taken the last one. Any transfer
  can resume from an offset. The data is base64 in JSON, a third bigger than raw; the simplicity
  is worth it at these sizes. (§7.5 describes downloads and uploads as streams; this is a
  proposed change to the spec.)
- **Downloads are plain HTTP GETs with Range support**, so a browser's own download manager can
  resume them. The server checks the file's size and modification time on every chunk, and
  stops rather than send a mixture of two versions of a file that changes during a download.
- **Uploads resume after anything**: the browser sends pieces of up to 8 MB, and the agent writes
  them to a hidden partial file beside the destination, named from the destination and the size.
  If the connection drops, or the agent restarts, beginning the same upload again finds the
  partial file and says how much arrived. A chunk must start where the upload is, so a chunk
  repeated after a lost answer is refused, not written twice. The file appears at its
  destination only when complete, never half-written. An upload won't replace an existing file
  unless asked.
- **SEC-25**: `[files] allowed_paths` in the policy file. Every path is checked after resolving
  symbolic links, so a link inside an allowed folder can't lead outside it, and `..` can't climb
  out. A new file's folder is resolved and checked the same way. Deleting a link removes the link
  and never follows it. The allowed folders themselves can't be deleted or moved, and browsing
  can't go above them. The checks are fuzzed.
- **Text editing** refuses binary files and files over 1 MB, keeps the file's mode, replaces the
  file in one step (write beside it, then rename), and refuses to save over a file that changed
  since it was opened.
- **Permissions and audit**: only roles with `manage_files` (operators and admins) can use the file
  manager, listing included, since a listing reveals as much as a file. Downloads, uploads,
  folder creation, renames, deletes and text saves are audited with their paths and sizes.

## Consequences

- Between the check of a path and its use, a local user on the machine could swap a folder for
  a link. The agent can't fully close this race without opening each path component itself;
  SEC-25 limits the server, which is its purpose, not local users, who have their own access.
- Throughput is one chunk per round trip: fine on a LAN, slower on a distant link. Asking for
  a few chunks at once is a possible later improvement.
- A cancelled upload's partial file is deleted; one abandoned without cancelling stays as a
  hidden `.<name>.<id>.sentinel-upload` file until the upload is resumed or someone removes it.

## The Files tab

- The folder, and the file being edited, are in the address (`?path=`, `&edit=`), so a reload or
  a shared link opens the same place, and closing the editor returns to the file's folder.
- A folder can hold thousands of entries (the agent sends at most 5,000 and says when it left
  some out); the list renders only those on screen, as the containers list does.
- Uploads show each file's progress. A failed piece resumes from what the machine has, up to
  three times, then offers "Resume"; a file that already exists offers "Replace it".
