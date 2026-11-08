// SPDX-License-Identifier: Apache-2.0

package logs_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/logs"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
	"github.com/aazam476/sentinel/internal/testutil/fakerunner"
)

// now is the fake time of every test: 12:00 UTC, Unix 1794139200.
func now() *fakeclock.Clock { return fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)) }

func milli(hour, minute, second, nano int) int64 {
	return time.Date(2026, 11, 8, hour, minute, second, nano, time.UTC).UnixMilli()
}

func TestJournald(t *testing.T) {
	t.Run("reads the last hour newest first, with every level and message shape", func(t *testing.T) {
		runner := fakerunner.New(t).ExpectFile("journalctl --output=json --no-pager --reverse --lines=201 --since=@1794135600",
			"testdata/journald-255.jsonl")
		got, err := logs.NewJournald(runner, now()).Query(t.Context(), protocol.LogQueryPayload{})
		if err != nil {
			t.Fatal(err)
		}
		want := []protocol.LogEntry{
			{Time: milli(12, 0, 0, 123e6), Level: protocol.LogInfo, Source: "systemd", Message: "Started nginx.service - A high performance web server and a reverse proxy server."},
			{Time: milli(11, 59, 59, 0), Level: protocol.LogError, Source: "nginx", Message: "2026/11/08 12:00:00 [emerg] 4242#4242: bind() to 0.0.0.0:80 failed (98: Address already in use)"},
			{Time: milli(11, 59, 58, 500e6), Level: protocol.LogWarning, Source: "kernel", Message: "EXT4-fs (sda1): warning: mounting fs with errors, running e2fsck is recommended"},
			{Time: milli(11, 59, 58, 0), Level: protocol.LogNotice, Source: "cron.service", Message: "CRON (root) \uFFFD"},
			{Time: milli(11, 59, 57, 0), Level: protocol.LogDebug, Source: "sshd", Message: "(message too large to show)"},
			{Time: milli(11, 59, 56, 0), Level: protocol.LogError, Source: "kernel", Message: "Kernel panic - not syncing: 日本語 ✓"},
		}
		if got.Truncated || len(got.Entries) != len(want) {
			t.Fatalf("got %d entries (truncated %v): %+v", len(got.Entries), got.Truncated, got.Entries)
		}
		for i := range want {
			if got.Entries[i] != want[i] {
				t.Errorf("entry %d:\n got %+v\nwant %+v", i, got.Entries[i], want[i])
			}
		}
	})

	t.Run("filters by unit, level and start, and reports more entries than the limit", func(t *testing.T) {
		runner := fakerunner.New(t).ExpectFile(
			"journalctl --output=json --no-pager --reverse --lines=3 --since=@1794139000 --priority=4 --unit=nginx.service",
			"testdata/journald-255.jsonl")
		got, err := logs.NewJournald(runner, now()).Query(t.Context(), protocol.LogQueryPayload{
			Service: "nginx.service", MinLevel: protocol.LogWarning, Limit: 2, Since: 1794139000_000})
		if err != nil || len(got.Entries) != 2 || !got.Truncated {
			t.Errorf("got %d entries, truncated %v (%v)", len(got.Entries), got.Truncated, err)
		}
	})

	t.Run("maps every level to a journald priority", func(t *testing.T) {
		priorities := map[protocol.LogLevel]string{protocol.LogError: "3", protocol.LogNotice: "5", protocol.LogInfo: "6", protocol.LogDebug: "7"}
		for level, priority := range priorities {
			runner := fakerunner.New(t).Expect("journalctl --output=json --no-pager --reverse --lines=201 --since=@1794135600 --priority="+priority, "", nil)
			got, err := logs.NewJournald(runner, now()).Query(t.Context(), protocol.LogQueryPayload{MinLevel: level})
			if err != nil || got.Entries == nil {
				t.Errorf("%s: %+v (%v)", level, got, err)
			}
		}
	})

	t.Run("reports a machine without journald", func(t *testing.T) {
		runner := fakerunner.New(t).Expect("journalctl --output=json --no-pager --reverse --lines=201 --since=@1794135600", "", command.ErrNotFound)
		if _, err := logs.NewJournald(runner, now()).Query(t.Context(), protocol.LogQueryPayload{}); !errors.Is(err, logs.ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestQueryChecks(t *testing.T) {
	cases := []struct {
		name  string
		query protocol.LogQueryPayload
	}{
		{"refuses a limit over 1,000", protocol.LogQueryPayload{Limit: 1001}},
		{"refuses a negative limit", protocol.LogQueryPayload{Limit: -1}},
		{"refuses an unknown level", protocol.LogQueryPayload{MinLevel: "critical"}},
		{"refuses a service name that would read as an option", protocol.LogQueryPayload{Service: "--merge"}},
		{"refuses a service name with a quote", protocol.LogQueryPayload{Service: "x' or '1'='1"}},
	}
	readers := map[string]logs.Reader{
		"journald": logs.NewJournald(fakerunner.New(t), now()), "unified log": logs.NewUnifiedLog(fakerunner.New(t), now()),
		"event log": logs.NewEventLog(fakerunner.New(t), now()),
	}
	for _, c := range cases {
		for name, reader := range readers {
			t.Run(name+" "+c.name, func(t *testing.T) {
				if _, err := reader.Query(t.Context(), c.query); !errors.Is(err, logs.ErrInvalidQuery) {
					t.Errorf("err = %v", err)
				}
			})
		}
	}
}

func TestUnifiedLog(t *testing.T) {
	t.Run("reads the last hour at every level, newest first, skipping the summary", func(t *testing.T) {
		runner := fakerunner.New(t).ExpectFile("log show --style ndjson --start 2026-11-08 11:00:00+0000 --info --debug",
			"testdata/macos-14-log-show.ndjson")
		got, err := logs.NewUnifiedLog(runner, now()).Query(t.Context(), protocol.LogQueryPayload{})
		if err != nil {
			t.Fatal(err)
		}
		want := []protocol.LogEntry{
			{Time: milli(12, 0, 0, 250e6), Level: protocol.LogInfo, Source: "Example", Message: "loaded configuration"},
			{Time: milli(12, 0, 0, 0), Level: protocol.LogDebug, Source: "com.example.app", Message: "cache hit"},
			{Time: milli(11, 59, 59, 500e6), Level: protocol.LogError, Source: "kernel", Message: "kernel fault in driver"},
			{Time: milli(11, 59, 59, 0), Level: protocol.LogError, Source: "mDNSResponder", Message: "mDNSResponder: failed to resolve 'example.invalid'"},
			{Time: milli(11, 59, 58, 123e6), Level: protocol.LogNotice, Source: "sshd", Message: "Accepted publickey for admin from 192.0.2.10 port 51234 ssh2"},
		}
		if len(got.Entries) != len(want) {
			t.Fatalf("got %+v", got.Entries)
		}
		for i := range want {
			if got.Entries[i] != want[i] {
				t.Errorf("entry %d:\n got %+v\nwant %+v", i, got.Entries[i], want[i])
			}
		}
	})

	cases := []struct {
		name    string
		query   protocol.LogQueryPayload
		command string
	}{
		{"errors only, for one job", protocol.LogQueryPayload{MinLevel: protocol.LogError, Service: "com.openssh.sshd"},
			`log show --style ndjson --start 2026-11-08 11:00:00+0000 --predicate (messageType == error OR messageType == fault) AND (subsystem == "com.openssh.sshd" OR process == "sshd")`},
		{"notices and above", protocol.LogQueryPayload{MinLevel: protocol.LogNotice},
			"log show --style ndjson --start 2026-11-08 11:00:00+0000"},
		{"info and above", protocol.LogQueryPayload{MinLevel: protocol.LogInfo},
			"log show --style ndjson --start 2026-11-08 11:00:00+0000 --info"},
	}
	for _, c := range cases {
		t.Run("builds the command for "+c.name, func(t *testing.T) {
			runner := fakerunner.New(t).Expect(c.command, "", nil)
			if _, err := logs.NewUnifiedLog(runner, now()).Query(t.Context(), c.query); err != nil {
				t.Error(err)
			}
		})
	}

	t.Run("reports log show failing", func(t *testing.T) {
		runner := fakerunner.New(t).Expect("log show --style ndjson --start 2026-11-08 11:00:00+0000 --info --debug", "", errors.New("exit status 64"))
		if _, err := logs.NewUnifiedLog(runner, now()).Query(t.Context(), protocol.LogQueryPayload{}); err == nil {
			t.Error("Query returned nil")
		}
	})
}

func TestEventLog(t *testing.T) {
	query := func(log, filter string, count int) string {
		return "wevtutil qe " + log + " /rd:true /c:" + itoa(count) + " /f:RenderedXml /q:*[System[" + filter + "]]"
	}
	since := "TimeCreated[@SystemTime>='2026-11-08T11:00:00.000Z']"

	t.Run("merges System and Application, newest first", func(t *testing.T) {
		runner := fakerunner.New(t).
			ExpectFile(query("System", since, 201), "testdata/windows-2022-system.xml").
			ExpectFile(query("Application", since, 201), "testdata/windows-2022-application.xml")
		got, err := logs.NewEventLog(runner, now()).Query(t.Context(), protocol.LogQueryPayload{})
		if err != nil {
			t.Fatal(err)
		}
		want := []protocol.LogEntry{
			{Time: milli(12, 0, 0, 123e6), Level: protocol.LogInfo, Source: "Service Control Manager", Message: "The Windows Update service entered the running state."},
			{Time: milli(11, 59, 30, 0), Level: protocol.LogInfo, Source: "MSSQL$SQLEXPRESS", Message: "Starting up database 'master'."},
			{Time: milli(11, 59, 0, 0), Level: protocol.LogError, Source: "Service Control Manager", Message: "The Print Spooler service terminated unexpectedly.  It has done this 1 time(s)."},
			{Time: milli(11, 58, 0, 0), Level: protocol.LogError, Source: "Microsoft-Windows-Kernel-Power", Message: "The system has rebooted without cleanly shutting down first."},
			{Time: milli(11, 57, 0, 0), Level: protocol.LogWarning, Source: "NoRenderer", Message: "Event 9 (no message text)"},
			{Time: milli(11, 50, 0, 0), Level: protocol.LogDebug, Source: "Verbose", Message: "debug detail"},
		}
		if len(got.Entries) != len(want) {
			t.Fatalf("got %+v", got.Entries)
		}
		for i := range want {
			if got.Entries[i] != want[i] {
				t.Errorf("entry %d:\n got %+v\nwant %+v", i, got.Entries[i], want[i])
			}
		}
	})

	cases := []struct {
		name   string
		query  protocol.LogQueryPayload
		filter string
	}{
		{"errors from one service", protocol.LogQueryPayload{MinLevel: protocol.LogError, Service: "MSSQL$SQLEXPRESS"},
			since + " and (Level=1 or Level=2) and Provider[@Name='MSSQL$SQLEXPRESS']"},
		{"warnings", protocol.LogQueryPayload{MinLevel: protocol.LogWarning}, since + " and (Level=1 or Level=2 or Level=3)"},
		{"info", protocol.LogQueryPayload{MinLevel: protocol.LogInfo}, since + " and (Level=0 or Level=1 or Level=2 or Level=3 or Level=4)"},
	}
	for _, c := range cases {
		t.Run("builds the query for "+c.name, func(t *testing.T) {
			runner := fakerunner.New(t).Expect(query("System", c.filter, 201), "", nil).Expect(query("Application", c.filter, 201), "", nil)
			if _, err := logs.NewEventLog(runner, now()).Query(t.Context(), c.query); err != nil {
				t.Error(err)
			}
		})
	}

	t.Run("reports wevtutil failing", func(t *testing.T) {
		runner := fakerunner.New(t).Expect(query("System", since, 201), "", command.ErrNotFound)
		if _, err := logs.NewEventLog(runner, now()).Query(t.Context(), protocol.LogQueryPayload{}); !errors.Is(err, logs.ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestNew(t *testing.T) {
	t.Run("picks the system log for each system", func(t *testing.T) {
		readers := map[string]logs.Reader{"linux": logs.Journald{}, "darwin": logs.UnifiedLog{}, "windows": logs.EventLog{}}
		for goos, want := range readers {
			got := logs.New(goos, fakerunner.New(t), now())
			if typeName(got) != typeName(want) {
				t.Errorf("%s: %T", goos, got)
			}
		}
	})

	t.Run("reports systems it doesn't know as unsupported", func(t *testing.T) {
		if _, err := logs.New("plan9", fakerunner.New(t), now()).Query(t.Context(), protocol.LogQueryPayload{}); !errors.Is(err, logs.ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})
}
