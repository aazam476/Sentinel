# 0051 — The agent owns its folders, and trusts only a policy file administrators own

**Date:** 2026-11-17 · **Status:** Accepted

## Context

SEC-20 keeps the agent's key and identity readable by root or SYSTEM only, and SEC-22 makes the
local policy something only an administrator of the machine can change. On Windows the agent
reset its state folder's access list but not its owner, and trusted any policy file. A standard
user who created `C:\ProgramData\Sentinel` before the agent was installed kept ownership, and an
owner can always change the access list back, then replace the policy or read the key. On Unix
an existing folder kept its owner too, and files left in it kept their modes.

## Decision

- **State folder, both platforms:** the agent refuses a folder that is a link (or, on Windows, a
  junction or other reparse point), then makes it and everything in it owned by the agent's
  account (root; Administrators on Windows) with access for that account only. Links inside are
  refused, never followed; on Unix the changes go through `os.Root`.
- **Policy file on Windows:** it must be owned by SYSTEM, Administrators or TrustedInstaller, and
  no other account may hold a right to change it, its access list or its owner, or to delete it.
  The rules are checked on the file's SDDL, so they are tested on every platform; Windows reads
  the descriptor. A refused file stops the agent with the `icacls` command that fixes it.

## Consequences

- A machine where someone tampered with these folders before the install stops with an error
  saying what to remove, instead of running with access they control.
- The Windows-specific calls are tested in CI on Windows only.
