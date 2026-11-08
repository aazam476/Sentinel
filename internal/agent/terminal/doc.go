// SPDX-License-Identifier: Apache-2.0

// Package terminal runs the shells behind the web UI's terminal (§9.2).
//
// On Linux and macOS a shell runs in a pseudo-terminal (PTY); on Windows in
// a pseudo-console (ConPTY). The Manager keeps track of them: it starts a
// shell when the server opens a terminal stream, copies the shell's output
// to the stream and the stream's input to the shell, and resizes it.
//
// When the viewer goes away without closing the terminal (a dropped
// browser, or the agent's own connection dropping), the shell keeps running
// for ResumeWindow, remembering its recent output. A new stream can
// reattach within that time and is sent the recent output first, so the
// screen comes back as it was. After that, or when the user closes the
// terminal, the shell is ended.
package terminal
