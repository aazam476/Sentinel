// SPDX-License-Identifier: Apache-2.0

package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/release"
)

const (
	// ConnectWithin is how long a new version has to connect, in its trial
	// and after it is installed, before it is rolled back (SEC-32).
	ConnectWithin = 5 * time.Minute
	// KeepPrevious is how long the previous binary is kept once the new one
	// has connected.
	KeepPrevious = 24 * time.Hour
	// maxBuildSize bounds a download: far above any real agent binary.
	maxBuildSize = 512 << 20
	// stateFile records an installed update, in the agent's state folder.
	stateFile = "update.json"
	// TrialFlag tells `sentinel-agent run` it is a trial: write to the file
	// it names once connected, then exit.
	TrialFlag = "update-trial"
)

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Options configures an Updater.
type Options struct {
	// Executable is the running agent binary.
	Executable string
	// StateDir is the agent's state folder, for the update record.
	StateDir string
	// ServerURL is the server the agent is enrolled with; builds are only
	// fetched from it.
	ServerURL string
	// HTTPClient fetches builds; it trusts the server as the agent's
	// connection does.
	HTTPClient *http.Client
	// Keys are the trusted release keys; nil uses the compiled-in ones.
	Keys []ed25519.PublicKey
	// Version is the running version; OS and Arch the machine's.
	Version, OS, Arch string
	// RunTrial runs a new build as a trial with the given arguments and
	// returns once it has exited.
	RunTrial func(ctx context.Context, executable string, args []string) error
	// TrialArgs are the `run` arguments for the trial (the file locations).
	TrialArgs []string
	// MaxSize bounds a download; 0 means 512 MiB.
	MaxSize int64
	Clock   Clock
	Logger  *slog.Logger
}

// Updater updates the agent.
type Updater struct {
	options Options
}

// New returns an Updater.
func New(options Options) *Updater {
	if options.Keys == nil {
		options.Keys = release.TrustedKeys()
	}
	return &Updater{options: options}
}

// record is what update.json holds about the last update.
type record struct {
	From        string `json:"from"`
	To          string `json:"to"`
	InstalledAt int64  `json:"installedAt"`
	ConfirmedAt int64  `json:"confirmedAt,omitempty"`
	// Report is a result still to tell the server, once connected.
	Report *protocol.AgentUpdateResultPayload `json:"report,omitempty"`
}

// versionPattern is what a version the server asks for may look like.
var versionPattern = regexp.MustCompile(`^v?[0-9A-Za-z.+-]{1,64}$`)

// pathPattern is where a build may be fetched from on the server.
var pathPattern = regexp.MustCompile(`^/install/[A-Za-z0-9._/+-]+$`)

// Update runs a whole update: fetch, check, trial, install. pause stops the
// agent's own connection for the trial and returns a function that resumes
// it. On success the caller restarts the agent; on failure it carries on
// with the returned error to report.
func (u *Updater) Update(ctx context.Context, request protocol.AgentUpdatePayload, pause func() (resume func())) error {
	if request.Version == u.options.Version {
		return fmt.Errorf("this agent already runs %s", request.Version)
	}
	candidate, err := u.fetch(ctx, request)
	if err != nil {
		return err
	}
	resume := pause()
	if err := u.trial(ctx, candidate); err != nil {
		resume()
		_ = os.Remove(candidate) // a failed build isn't kept
		return err
	}
	if err := u.install(candidate, request.Version); err != nil {
		resume()
		_ = os.Remove(candidate)
		return err
	}
	return nil
}

func (u *Updater) candidatePath() string {
	return u.options.Executable + ".new"
}

func (u *Updater) previousPath() string {
	return u.options.Executable + ".previous"
}

// fetch downloads the build next to the running binary and checks its
// signature, returning where it is.
func (u *Updater) fetch(ctx context.Context, request protocol.AgentUpdatePayload) (string, error) {
	if !versionPattern.MatchString(request.Version) || !pathPattern.MatchString(request.Path) || strings.Contains(request.Path, "..") {
		return "", errors.New("the update request is malformed")
	}
	base, err := url.Parse(u.options.ServerURL)
	if err != nil {
		return "", fmt.Errorf("read the server address: %w", err)
	}
	source := base.JoinPath(request.Path)
	// The URL is the server's own, already parsed, so the request is valid.
	httpRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	response, err := u.options.HTTPClient.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("download the update: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download the update: the server answered %s", response.Status)
	}
	build, err := io.ReadAll(io.LimitReader(response.Body, u.maxSize()+1))
	if err != nil {
		return "", fmt.Errorf("download the update: %w", err)
	}
	if int64(len(build)) > u.maxSize() {
		return "", errors.New("the update is larger than any agent build")
	}
	// Checked before it is written, so an unverified build never lands on
	// disk as an executable (SEC-31).
	want := release.Expect{Name: "sentinel-agent", Version: request.Version, OS: u.options.OS, Arch: u.options.Arch}
	if _, err := release.VerifyWith(bytes.NewReader(build), []byte(request.Signature), want, u.options.Keys); err != nil {
		return "", fmt.Errorf("the update was refused: %w", err)
	}
	candidate := u.candidatePath()
	_ = os.Remove(candidate)                                      // left by an update that was interrupted
	if err := os.WriteFile(candidate, build, 0o755); err != nil { //nolint:gosec // G306: an executable, beside the agent's own
		_ = os.Remove(candidate)
		return "", fmt.Errorf("save the update: %w", err)
	}
	return candidate, nil
}

func (u *Updater) maxSize() int64 {
	if u.options.MaxSize > 0 {
		return u.options.MaxSize
	}
	return maxBuildSize
}

// trial runs the new build until it has connected, or fails, or its time
// is up.
func (u *Updater) trial(ctx context.Context, candidate string) error {
	marker := filepath.Join(u.options.StateDir, "update-trial")
	_ = os.Remove(marker)
	defer func() { _ = os.Remove(marker) }()
	ctx, cancel := context.WithTimeout(ctx, ConnectWithin)
	defer cancel()
	args := append(append([]string{"run"}, u.options.TrialArgs...), "--"+TrialFlag, marker)
	err := u.options.RunTrial(ctx, candidate, args)
	if content, readErr := os.ReadFile(marker); readErr == nil && strings.HasPrefix(string(content), "connected") { //nolint:gosec // G304: in the agent's state folder
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("the new version didn't connect within %s", ConnectWithin)
	}
	if err != nil {
		return fmt.Errorf("the new version failed before connecting: %w", err)
	}
	return errors.New("the new version stopped before connecting")
}

// install keeps the running binary as the previous one, moves the new one
// into its place and records the update.
func (u *Updater) install(candidate, version string) error {
	_ = os.Remove(u.previousPath()) // from an update more than a day ago, or one rolled back
	if err := os.Rename(u.options.Executable, u.previousPath()); err != nil {
		return fmt.Errorf("keep the previous version: %w", err)
	}
	if err := os.Rename(candidate, u.options.Executable); err != nil {
		_ = os.Rename(u.previousPath(), u.options.Executable)
		return fmt.Errorf("install the new version: %w", err)
	}
	return u.write(record{From: u.options.Version, To: version, InstalledAt: u.options.Clock.Now().UnixMilli()})
}

func (u *Updater) read() (record, bool) {
	content, err := os.ReadFile(filepath.Join(u.options.StateDir, stateFile))
	if err != nil {
		return record{}, false
	}
	var r record
	if json.Unmarshal(content, &r) != nil {
		return record{}, false
	}
	return r, true
}

func (u *Updater) write(r record) error {
	content, _ := json.Marshal(r) // a struct of strings and numbers always encodes
	if err := os.WriteFile(filepath.Join(u.options.StateDir, stateFile), content, 0o600); err != nil {
		return fmt.Errorf("record the update: %w", err)
	}
	return nil
}

// Watch returns whether this start is a new version that hasn't connected
// yet: the caller must then call Rollback if Connected hasn't been called
// within ConnectWithin. It also tidies up after updates that settled.
func (u *Updater) Watch() bool {
	r, found := u.read()
	if !found {
		return false
	}
	if r.ConfirmedAt == 0 && r.To == u.options.Version {
		return true
	}
	_ = os.Remove(u.options.Executable + ".failed") // a rolled-back build, no longer running
	if r.ConfirmedAt != 0 && u.options.Clock.Now().UnixMilli()-r.ConfirmedAt > KeepPrevious.Milliseconds() {
		_ = os.Remove(u.previousPath())
	}
	return false
}

// Connected records that this version has connected, and returns a result
// to tell the server, if one is due.
func (u *Updater) Connected() *protocol.AgentUpdateResultPayload {
	r, found := u.read()
	if !found {
		return nil
	}
	report := r.Report
	if r.ConfirmedAt == 0 && r.To == u.options.Version {
		r.ConfirmedAt = u.options.Clock.Now().UnixMilli()
		report = &protocol.AgentUpdateResultPayload{Version: r.To, OK: true}
	}
	r.Report = nil
	if err := u.write(r); err != nil {
		u.options.Logger.Warn("could not record the update", "error", err)
	}
	return report
}

// Rollback puts the previous binary back, after the new version failed to
// connect, and records a result for it to report. The caller restarts.
func (u *Updater) Rollback() error {
	r, _ := u.read()
	// The failed build is running: Windows lets a running program be
	// renamed, not replaced, so it is moved aside first.
	failed := u.options.Executable + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(u.options.Executable, failed); err != nil {
		return fmt.Errorf("move the failed version aside: %w", err)
	}
	if err := os.Rename(u.previousPath(), u.options.Executable); err != nil {
		_ = os.Rename(failed, u.options.Executable)
		return fmt.Errorf("put the previous version back: %w", err)
	}
	r.Report = &protocol.AgentUpdateResultPayload{
		Version: r.To, RolledBack: true,
		Error: fmt.Sprintf("%s didn't connect within %s after it was installed, so %s was put back", r.To, ConnectWithin, r.From),
	}
	r.To = r.From
	r.ConfirmedAt = u.options.Clock.Now().UnixMilli()
	return u.write(r)
}

// Failed records a result to report for an update that didn't install.
func (u *Updater) Failed(version string, cause error) {
	r, _ := u.read()
	r.Report = &protocol.AgentUpdateResultPayload{Version: version, Error: cause.Error()}
	if err := u.write(r); err != nil {
		u.options.Logger.Warn("could not record the failed update", "error", err)
	}
}
