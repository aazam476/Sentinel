# Glossary

| Term | Meaning |
|---|---|
| ADR | Architecture Decision Record, a short document in `docs/decisions/` explaining a decision |
| Agent | `sentinel-agent`, the program on each managed machine |
| ConPTY | Windows pseudo-console API, the Windows equivalent of a PTY |
| Desktop helper | The agent running in a second mode inside the user's GUI session for remote desktop |
| Enrollment | The first-time process where an agent registers with the server |
| Handshake | The signed exchange at the start of every agent connection proving both sides' identity |
| Policy file | A root-owned file on each machine that limits what the server can make the agent do |
| PTY | Pseudo-terminal; lets a program behave like an interactive terminal |
| Scrub | A ZFS operation that reads all data to find and repair corruption |
| Server | `sentinel-server`, the central program with the web UI |
| Stack | A set of containers defined by a Docker Compose file |
| Step-up auth | Asking for 2FA again before a sensitive action |
| Stream | A long-running data channel inside the WebSocket (terminal, file transfer, script output) |
| TCC | macOS "Transparency, Consent and Control", the permission system for screen recording etc. |
| TURN | A relay server for WebRTC when direct connections fail |
| WebRTC | Browser technology for real-time video; used for remote desktop |
