# Security self-review

Every security requirement (SEC-xx in the specification), how Sentinel meets it, and the
tests that prove it. Tests are named after the item they cover (`TestSEC07_...` in Go, "SEC-07
..." in the web and end-to-end tests), and `make check-sec-coverage` fails if a listed item has
none. Reviewed for `v0.1.0-beta.1`.

Where something is limited or left to a maintainer, the **Limits** line says so.

## Authentication

**SEC-01 Setup token (removed).** First-time setup no longer asks for a token (ADR 0037).
Setup is open only while no user exists; the first admin is created in one transaction with
their confirmed TOTP secret, after which setup refuses (`internal/server/auth/setup.go`).
*Tests:* `TestSetupIsOpenOnlyUntilAnAdminExists`, `TestSEC02_SetupOverHTTP`,
`TestAFreshServerWaitsForTheFirstAdminWithoutASetupToken`, Playwright "a fresh install sends
every page to setup and signs no one in".
**Limits:** whoever reaches a fresh server first can create its admin. INSTALL.md says to set
it up before exposing the port.

**SEC-02 First admin.** Setup takes a username and a password of at least 12 characters not on
the bundled common-password list, then requires TOTP enrollment and shows 10 one-time recovery
codes (`auth/setup.go`, `validation.go`, `recoverycodes.go`).
*Tests:* `TestSEC02_SetupCreatesAnAdminWithTOTPAndRecoveryCodes`,
`TestSEC02_SetupChecksThePasswordAndTOTP`, component and Playwright tests of the setup page.

**SEC-03 Argon2id.** 64 MiB, 3 iterations, parallelism 2, at most two hashes at once so a burst
of logins can't exhaust memory; unknown usernames are checked against a dummy hash so timing
doesn't reveal them (`auth/passwords.go`, ADR 0010).
*Tests:* `TestSEC03_PasswordsAreHashedWithArgon2id`.
*Limits:* the tests hash with the full settings, which makes the package slow on CI; the test
timeout is raised to 20 minutes meanwhile.

**SEC-04 Two-factor authentication (changed: optional by default).** Each user chooses whether
to add TOTP or a passkey, from setup or their account; a user who has one is always asked for
it. An admin who uses 2FA can turn on **Require it for everyone**: users without a second
factor are then signed out and must enroll before anything else, and nobody can remove their
last second factor. Passkeys can replace TOTP (`auth/twofactor.go`, `enrollment.go`,
`passkeys.go`, `passkeyassertion.go`, ADR 0039).
*Tests:* `TestSEC04_TwoFactorIsEachUsersChoiceByDefault`, `TestSEC04_AnAdminCanRequireTwoFactor`,
`TestSEC04_SetupCantSkip2FAWhereItIsRequired`, `TestSEC04_WithoutTheRequirementTheLastPasskeyCanGo`,
`TestSEC04_TwoFactorIsRequiredToSignIn`, `TestSEC04_UsersWithout2FAMustEnrollFirst`,
`TestSEC04_PasskeysCanReplaceTOTP`, `TestSEC04_TwoFactorIsOptionalOverHTTP`,
`TestSEC04_RequiringTwoFactorOverHTTP`, `TestSEC04_PasskeysOverHTTP`, Playwright sign-in tests
with a virtual authenticator and "M6: an admin adds a user, requires 2FA of them, and disables
them".
*Limits:* until an admin requires it, an account without 2FA is protected by its password
alone (still Argon2id-hashed, rate-limited and step-up-checked).

**SEC-05 Rate limiting.** Per IP and per username, exponential backoff, a 15-minute lock after
10 failures, every failure audited; unknown usernames are limited too (`auth/ratelimit.go`,
`ratelimiting.go`).
*Tests:* `TestSEC05_FailedLoginsBackOffExponentially`, `TestSEC05_LocksAccountAfterTenFailures`,
`TestSEC05_UnknownUsernamesAreLimitedToo`, `TestSEC05_RateLimitingOverHTTP`.

**SEC-06 Sessions.** 256-bit random tokens in `HttpOnly`, `Secure`, `SameSite=Strict` cookies,
stored only as hashes; 12-hour idle and 7-day absolute timeouts; users list and revoke them
(`auth/sessions.go`, `tokens.go`).
*Tests:* `TestSEC06_SessionsExpire`, `TestSEC06_SessionTokensAreStoredHashed`,
`TestSEC06_UsersCanListAndRevokeTheirSessions`, `TestSEC06_SessionsOverHTTP`.

**SEC-07 Step-up.** Opening a terminal or a desktop session, running a script on more than one
machine, changing users, roles or the 2FA requirement, and adding or removing a second factor
need a second factor checked within 15 minutes (or, for a user without one, the password
re-entered); the UI asks for it in place and retries (`auth/stepup.go`, `twofactor.go`). For
scripts that covers the three ways a person can put one on several machines: running it,
saving a schedule that is on and reaches more than one, and changing a script such a schedule
runs (`internal/server/scripts/stepup.go`, ADR 0043).
*Tests:* `TestSEC07_OpeningATerminalNeedsARecentSecondFactor`,
`TestSEC07_OpeningADesktopNeedsARecentSecondFactor`, `TestSEC07_ChangingUsersNeedsARecentSecondFactor`,
`TestSEC07_StepUpOverHTTP`, `TestSEC07_RunningAScriptOnMoreThanOneMachineNeedsARecentSecondFactor`,
`TestSEC07_ScheduleThatReachesMoreThanOneMachineNeedsARecentSecondFactor`,
`TestSEC07_ChangingAScriptScheduledOnMoreThanOneMachineNeedsARecentSecondFactor`,
`TestSEC07_AScriptOnMoreThanOneMachineNeedsARecentSecondFactorOverHTTP`, the password step-up in
`TestSEC04_TwoFactorIsEachUsersChoiceByDefault` and `TestSEC04_TwoFactorIsOptionalOverHTTP`,
`TestSEC05_PasswordStepUpIsRateLimited`, component tests of the step-up dialog in each place.
*Limits:* a script on one machine, the file manager, Docker and stacks need no step-up,
though each can do what a terminal can; SEC-07 doesn't list them.

**SEC-08 Local recovery.** `sentinel-server reset-admin` works only as root or the data
directory's owner, and the admin must set up 2FA again (`cmd/sentinel-server/resetadmin.go`,
`internal/server/localaccess`).
*Tests:* `TestSEC08_OnlyRootOrTheDataOwnerMayResetTheAdmin`,
`TestSEC08_OnlyAccountsThatCanWriteTheDatabaseMayResetTheAdmin`,
`TestSEC08_ResetAdminRestoresAccessWith2FAStillRequired`, `TestSEC08_ResetAdminCommand`.

## Web security

**SEC-10 TLS by default.** A self-signed certificate is made on first start; ACME is an option;
plain HTTP is refused except on loopback or with `behind_tls_proxy`
(`internal/server/config/transport.go`, `servertls`, `httpserver/listen.go`).
*Tests:* `TestSEC10_TLSIsOnByDefaultAndPlainHTTPOnlyOnLoopbackOrBehindAProxy`,
`TestSEC10_SelfSignedCertificateIsCreatedOnFirstStart`, `TestSEC10_ServerUsesTLSByDefault`,
the CLI test refusing plain HTTP on every interface.

**SEC-11 Headers.** A strict Content-Security-Policy (no inline scripts or styles, no other
origins), `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, on every response
(`middleware/headers.go`).
*Tests:* `TestSEC11_SecurityHeadersAreOnEveryResponse`, every Playwright test fails on a CSP
violation.

**SEC-12 CSRF.** `SameSite=Strict` cookies plus an `Origin` check on every state-changing
request (`middleware/origin.go`, ADR 0007).
*Tests:* `TestSEC12_StateChangingRequestsFromOtherSitesAreRefused`,
`TestSEC12_CrossSiteRequestsAreRefusedOverHTTP`, the CLI test against the real binary.

**SEC-13 WebSocket origins.** Browser WebSocket upgrades (terminal, desktop, files) must come
from the public URL's origin; the agents' endpoint refuses any request with an `Origin`.
*Tests:* `TestSEC13_WebSocketUpgradesCheckTheOrigin`,
`TestSEC13_ATerminalWebSocketFromAnotherSiteIsRefused`, `TestSEC13_BrowsersCantOpenTheAgentEndpoint`.

**SEC-14 Reverse proxies.** `public_url`, `base_path`, and `X-Forwarded-*` believed only from
`trusted_proxies` (`middleware/basepath.go`, `clientinfo.go`).
*Tests:* `TestSEC14_ForwardedHeadersAreOnlyBelievedFromTrustedProxies`,
`TestSEC14_ServesUnderAConfiguredBasePath`, `TestSEC14_PageWorksUnderABasePath`.

**SEC-15 Input, SQL, shells.** Request bodies are decoded strictly (unknown fields refused,
sizes capped) and validated; every query uses parameters; external programs are run directly
with argument lists, never through a shell (`api/requests.go`, `internal/agent/command`).
Every parser of untrusted input is fuzzed (`make fuzz`).
*Tests:* `TestSEC15_RequestBodiesAreValidated`, the fuzz targets, golden-output tests of every
command the agent runs.

## Agent security

**SEC-20 Agent state.** The key and identity live in a directory only root (Linux, macOS) or
SYSTEM and Administrators (Windows, by ACL) can use (`internal/agent/identity`).
*Tests:* `TestSEC20_OnlyTheAgentsUserCanUseTheStateDirectory`,
`TestSEC20_OnlySystemAndAdministratorsCanUseTheStateDirectory` (on the Windows CI job).

**SEC-21 Mutual authentication and revocation.** Both sides sign the other's random challenge
with Ed25519 on every connection; revoking a machine closes its live connection at once
(`internal/protocol`, `internal/server/hub`).
*Tests:* `TestSEC21_HandshakeSignaturesProveBothSides`,
`TestSEC21_AnEnrolledAgentAndTheServerProveTheirIdentities`,
`TestSEC21_RevocationClosesTheLiveConnectionImmediately`, and the end-to-end enrollment test.

**SEC-22 Local policy.** `policy.toml`, owned by root, switches any capability off; the agent
refuses to start if anyone else can change it or it has a typo; the server can't change it and
the UI shows "Disabled by local policy" (`internal/agent/policy`). Refusals are audited as
denied.
*Tests:* `TestSEC22_ThePolicyFileSwitchesCapabilitiesOff`,
`TestSEC22_EverythingIsAllowedWithoutAPolicyFile`, the per-feature "SEC-22" refusals in the agent
core and API tests.

**SEC-23 Desktop consent.** With `desktop_requires_consent = true`, the logged-in user is shown
who asks and must click Allow; Deny, 25 seconds without an answer, or no one to ask refuses the
session (`internal/agent/core/notify.go`, `internal/agent/desktop/x11/notify.go`, ADR 0034).
On Windows and macOS the question goes to every connected Sentinel tray and the first answer
decides (`internal/agent/tray/server.go`, ADR 0038).
*Tests:* `TestSEC23_TheConsentPrompt` (clicking Allow and Deny on a real Xvfb),
`TestSEC23_NoOneToAsk`, `TestSEC23_ThePersonAtTheMachineAllowsRemoteDesktop`,
`TestSEC23_TheFirstTrayToAnswerDecides`, `TestSEC23_AnUnansweredQuestionGivesUp`,
`TestSEC23_TheTrayAsksBeforeRemoteDesktop`.
*Limits:* with no tray running on Windows or macOS, and on Wayland, the setting refuses every
desktop session (fails closed). Anyone signed in at a Windows machine can answer, not only
the user whose screen is shared. The Windows and macOS dialogs haven't been tried on real
machines.

**SEC-24 Session indicator.** With `show_session_indicator = true`, a bar at the top of the
screen names who is on the desktop and how many terminals are open, while any are.
On Windows and macOS the tray lists the open sessions, with who opened each, turns its icon
red with a dot and announces each session; the person at the machine can end any of them or
pause remote access.
*Tests:* `TestSEC24_TheSessionIndicator` (on Xvfb, and in the agent core following a desktop
session and a terminal), `TestSEC24_TheTrayListsSessionsAndEndsThem`, `TestSEC24_ShowNeedsATray`,
`TestTheTrayPausesRemoteAccess`.
*Limits:* where no one is logged in, or no tray is running, the agent logs that it couldn't
show it.

**SEC-25 File paths.** The policy's `allowed_paths` confine the file manager; paths are cleaned
and checked after resolving symlinks, so `..` and links can't leave them
(`internal/agent/files/paths.go`).
*Tests:* `TestSEC25_PathRestrictions`, policy parsing tests, the Playwright file manager test.

**SEC-26 No listening port.** The agent only dials out. The Windows and macOS desktop helper
talks to the agent over the child process's pipes, and remote desktop's WebRTC connections are
answered, not listened for. The tray's socket is a local Unix socket, which only users signed
in at the machine (Windows) or the console user and root (macOS) may use; Linux opens none.
*Tests:* `TestSEC26_TheAgentNeverListensOnANetworkPort` (scans the agent's source: the one
listener allowed is the tray's, and it must be `"unix"`), the helper protocol tests over pipes,
`TestListenOnlyServesPermittedUsers`.

## Updates and supply chain

**SEC-30 Signed releases.** Every release file is signed with an offline Ed25519 key whose
public half is compiled into both binaries (`internal/shared/release`, `cmd/sentinel-release`,
`scripts/release.sh`, ADR 0027).
*Tests:* `TestSEC30_ReleaseKeysAreCompiledIn`, signature format tests.
*Limits:* no production key is committed yet; until a maintainer adds one, builds trust only
the published development key and the release workflow refuses to run (see INSTALL.md).

**SEC-31 Verified updates.** The server checks a build's signature when fetching or uploading
it; the agent checks it again before installing; bad or missing signatures are refused and
reported (`internal/server/updates`, `internal/agent/update`).
*Tests:* `TestSEC31_UpdatesAreVerifiedBeforeInstalling`, `TestSEC31_HostedBuilds`,
`TestSEC31_Update`, the Playwright update test with a tampered build.

**SEC-32 Rollback.** The agent tries a new version while keeping the old one, switches only if
it connects, and a watchdog restores the old binary if the new one hasn't connected within 5
minutes; the result is reported (ADR 0028).
*Tests:* `TestSEC32_Rollback`, `TestSEC32_RecordResult`, the Playwright test with a build that
never connects.

**SEC-33 Reproducible builds, checksums, SBOMs.** Releases are built with `-trimpath` and fixed
build IDs; `SHA256SUMS` and an SPDX SBOM per binary are published (`scripts/release.sh`,
`internal/shared/release/sbom.go`).
*Tests:* `TestSEC33_SBOM`; the nightly job builds twice and compares (`scripts/check-reproducible.sh`).

**SEC-34 Verified installs.** The shell, PowerShell and server install scripts check the
downloaded binary's signature (`openssl`, or PowerShell's own Ed25519 check) before installing.
*Tests:* `TestSEC34_ShellInstallerVerifiesBeforeInstalling`,
`TestSEC34_PowerShellInstallerVerifiesBeforeInstalling`,
`TestSEC34_ServerInstallerVerifiesBeforeInstalling` (on Linux, macOS and Windows CI).

## Audit and secrets

**SEC-40 Audit log.** Append-only, each row chained to the last by SHA-256: sign-ins and
failures, terminal, desktop and file sessions, every state-changing action, user and role
changes, policy denials (`internal/server/audit`).
*Tests:* `TestSEC40_EntriesFormAHashChain`, `TestSEC40_AuditLogIsAppendOnly`, the audit checks in
each feature's API tests (`desktop.open`, `desktop.close`, ...).

**SEC-41 Viewing, export and verification.** Admins page through the log, export it as CSV or
JSON, and verify the chain in the UI or with `sentinel-server audit verify`.
*Tests:* `TestSEC41_VerifyDetectsTampering`, `TestSEC41_VerifyAcceptsAnUntouchedLog`,
`TestSEC41_AuditLogExports`, `TestSEC41_AuditLogOverHTTP`, Playwright "SEC-41".

**SEC-42 Secrets at rest.** SMTP passwords, registry credentials and webhook URLs are sealed
with AES-256-GCM using `<data dir>/secret.key` (0600), outside the database
(`internal/server/secrets`).
*Tests:* `TestSEC42_SecretsAreEncryptedWithAKeyOutsideTheDatabase`,
`TestSEC42_NotificationChannelSecretsAreStoredEncrypted`.

**SEC-43 No terminal recording.** Terminal sessions aren't recorded: the server relays bytes
without keeping them, and only the session's start, end, duration and byte counts are
audited. Recording, a later feature, doesn't exist.
*Tests:* none needed beyond the relay tests: there is no code that stores session content.

## Beyond the list

- **Remote desktop's TURN relay** issues credentials per session that expire after eight hours
  and refuses to relay to loopback, link-local, multicast and unspecified addresses, so a
  signed-in user can't use it to reach the server's own services or a cloud metadata endpoint
  (ADR 0033; `internal/server/turn` tests).
- **The clipboard** is shared only in sessions with control, never view-only, and capped at
  64 KiB (ADR 0035).
- **What can't be checked here:** the 72-hour soak test, the manual QA checklist and fresh
  installs on real Windows and macOS machines are for a maintainer before tagging the beta.
