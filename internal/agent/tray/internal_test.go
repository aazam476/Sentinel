// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordPauseReportsWhatItCantWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	s := NewServer(ServerOptions{PauseFile: filepath.Join(dir, "paused"), Logger: slog.New(slog.DiscardHandler)})
	if err := s.recordPause(true); err == nil {
		t.Error("recording a pause in a missing folder succeeded")
	}
	// A folder in the pause file's place can't be removed as a file.
	if err := os.MkdirAll(filepath.Join(dir, "paused", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.recordPause(false); err == nil {
		t.Error("clearing a pause that is a folder succeeded")
	}
	if err := NewServer(ServerOptions{}).recordPause(true); err != nil {
		t.Errorf("a pause kept in memory: %v", err)
	}
}

func TestAnnouncementsNameWhoAndWhat(t *testing.T) {
	tests := []struct {
		session Session
		want    string
	}{
		{Session{Kind: KindDesktop, User: "admin"}, "admin is connected to this screen through Sentinel"},
		{Session{Kind: KindTerminal}, "Someone opened a terminal on this machine through Sentinel"},
	}
	for _, test := range tests {
		if got := announcement(test.session); got != test.want {
			t.Errorf("announcement(%+v) = %q, want %q", test.session, got, test.want)
		}
	}
}

func TestPrepareSocketDirRefusesAFolderInTheSocketsPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tray.sock")
	if err := os.MkdirAll(filepath.Join(path, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := prepareSocketDir(path, 0o700); err == nil {
		t.Error("prepareSocketDir removed a folder full of files")
	}
	if _, err := Listen(path); err == nil {
		t.Error("Listen took the place of a folder")
	}
}

func TestDecodeRefusesAnIDThatIsntPlainText(t *testing.T) {
	if _, err := Decode([]byte(`{"type":"end","id":"a\u0007b"}`)); !errors.Is(err, ErrBadMessage) {
		t.Errorf("Decode = %v, want ErrBadMessage", err)
	}
}
