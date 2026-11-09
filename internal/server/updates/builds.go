// SPDX-License-Identifier: Apache-2.0

package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/shared/release"
)

// Database runs transactions (internal/server/store).
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
}

// AuditLog records changes in the same transaction as the change.
type AuditLog interface {
	RecordInTx(ctx context.Context, tx *sql.Tx, entry audit.Entry) error
}

// Hub reaches the agents (internal/server/hub).
type Hub interface {
	IsOnline(agentID int64) bool
	Request(ctx context.Context, agentID int64, action string, payload, result any) error
}

// Fleet lists the machines (internal/server/fleet).
type Fleet interface {
	AllAgents(ctx context.Context) ([]fleet.Agent, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Options configures a Service.
type Options struct {
	DataDir  string
	Database Database
	AuditLog AuditLog
	Hub      Hub
	Fleet    Fleet
	Clock    Clock
	Logger   *slog.Logger
	// Keys are the trusted release keys; nil uses the compiled-in ones.
	Keys []ed25519.PublicKey
	// ReleaseURL is where the latest release's files are, e.g.
	// https://github.com/<owner>/sentinel/releases/latest/download/.
	ReleaseURL string
	HTTPClient *http.Client
}

// Service hosts builds and updates agents.
type Service struct {
	options Options
}

// New returns a Service.
func New(options Options) *Service {
	if options.Keys == nil {
		options.Keys = release.TrustedKeys()
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	return &Service{options: options}
}

// Errors.
var (
	// ErrNoBuild means no build of that version is hosted for the machine.
	ErrNoBuild = errors.New("there is no agent build for that machine")
	// ErrNotFound means there is no such machine.
	ErrNotFound = errors.New("not found")
)

// maxBuildSize bounds an uploaded or fetched build.
const maxBuildSize = 512 << 20

// Build is one hosted agent build.
type Build struct {
	Version, OS, Arch string
	// File is its name, as served under /install/updates/<version>/.
	File    string
	Size    int64
	AddedAt int64
	// signature is its release signature file.
	signature []byte
}

// Path is where the build is served.
func (b Build) Path() string {
	return "/install/updates/" + b.Version + "/" + b.File
}

// fileName is a build's file name.
func fileName(manifest release.Manifest) string {
	name := fmt.Sprintf("sentinel-agent_%s_%s_%s", manifest.Version, manifest.OS, manifest.Arch)
	if manifest.OS == "windows" {
		name += ".exe"
	}
	return name
}

func (s *Service) folder() string {
	return filepath.Join(s.options.DataDir, "updates")
}

// Add checks a build against its signature (SEC-31) and hosts it.
func (s *Service) Add(ctx context.Context, actor auth.Session, request auth.RequestInfo, build io.Reader, signature []byte) (Build, error) {
	if err := auth.Authorize(actor, auth.PermissionManageMachines); err != nil {
		return Build{}, err
	}
	added, err := s.add(build, signature)
	if err != nil {
		return Build{}, err
	}
	return added, s.audit(ctx, userEntry(actor, request, "agent.build.add", map[string]any{
		"version": added.Version, "os": added.OS, "arch": added.Arch,
	}))
}

func (s *Service) add(build io.Reader, signature []byte) (Build, error) {
	content, err := io.ReadAll(io.LimitReader(build, maxBuildSize+1))
	if err != nil {
		return Build{}, fmt.Errorf("read the build: %w", err)
	}
	if len(content) > maxBuildSize {
		return Build{}, &InvalidError{"build", "the file is larger than any agent build"}
	}
	manifest, err := release.VerifyWith(bytes.NewReader(content), signature, release.Expect{Name: "sentinel-agent"}, s.options.Keys)
	if err != nil {
		return Build{}, &InvalidError{"signature", "the build's release signature doesn't check out: " + err.Error()}
	}
	folder := filepath.Join(s.folder(), manifest.Version)
	if err := os.MkdirAll(folder, 0o750); err != nil {
		return Build{}, fmt.Errorf("store the build: %w", err)
	}
	file := filepath.Join(folder, fileName(manifest))
	if err := os.WriteFile(file, content, 0o644); err != nil { //nolint:gosec // G306: served to anyone, like a release download
		return Build{}, fmt.Errorf("store the build: %w", err)
	}
	if err := os.WriteFile(file+".sig", signature, 0o644); err != nil { //nolint:gosec // G306: signatures are public
		return Build{}, fmt.Errorf("store the build: %w", err)
	}
	return Build{Version: manifest.Version, OS: manifest.OS, Arch: manifest.Arch, File: fileName(manifest),
		Size: int64(len(content)), AddedAt: s.options.Clock.Now().UnixMilli(), signature: signature}, nil
}

// InvalidError says what is wrong with a request.
type InvalidError struct {
	Field, Reason string
}

func (e *InvalidError) Error() string { return e.Field + ": " + e.Reason }

// Builds lists the hosted builds, newest version first. A file whose
// signature no longer checks out (the trusted keys changed) is left out.
func (s *Service) Builds() ([]Build, error) {
	signatures, err := filepath.Glob(filepath.Join(s.folder(), "*", "sentinel-agent_*.sig"))
	if err != nil {
		return nil, fmt.Errorf("list builds: %w", err)
	}
	builds := []Build{}
	for _, signatureFile := range signatures {
		if build, ok := s.readBuild(signatureFile); ok {
			builds = append(builds, build)
		}
	}
	slices.SortFunc(builds, func(a, b Build) int {
		if order := compareVersions(b.Version, a.Version); order != 0 {
			return order
		}
		return strings.Compare(a.File, b.File)
	})
	return builds, nil
}

func (s *Service) readBuild(signatureFile string) (Build, bool) {
	signature, err := os.ReadFile(signatureFile) //nolint:gosec // G304: in the server's own updates folder
	if err != nil {
		return Build{}, false
	}
	manifest, err := release.ParseSignature(signature, s.options.Keys)
	if err != nil {
		return Build{}, false
	}
	info, err := os.Stat(strings.TrimSuffix(signatureFile, ".sig"))
	if err != nil || filepath.Base(filepath.Dir(signatureFile)) != manifest.Version {
		return Build{}, false
	}
	return Build{Version: manifest.Version, OS: manifest.OS, Arch: manifest.Arch, File: fileName(manifest),
		Size: info.Size(), AddedAt: info.ModTime().UnixMilli(), signature: signature}, true
}

// compareVersions orders versions as semantic versions where they are,
// else as text.
func compareVersions(a, b string) int {
	if semver.IsValid(a) && semver.IsValid(b) {
		return semver.Compare(a, b)
	}
	return strings.Compare(a, b)
}

// buildFor is the build of version (the newest if "") for a machine.
func (s *Service) buildFor(osName, arch, version string) (Build, error) {
	builds, err := s.Builds()
	if err != nil {
		return Build{}, err
	}
	for _, build := range builds {
		if build.OS == osName && build.Arch == arch && (version == "" || build.Version == version) {
			return build, nil
		}
	}
	return Build{}, ErrNoBuild
}

// safeSegment is a version or file name the handler serves.
var safeSegment = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,120}$`)

// Handler serves hosted builds and their signatures under
// /install/updates/<version>/<file>, to anyone: they are public releases.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version, file := r.PathValue("version"), r.PathValue("file")
		if !safeSegment.MatchString(version) || !safeSegment.MatchString(file) || strings.HasPrefix(version, ".") || strings.HasPrefix(file, ".") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(s.folder(), version, file)) //nolint:gosec // G703: both parts are checked against safeSegment above
	})
}

func (s *Service) audit(ctx context.Context, entry audit.Entry) error {
	return s.options.Database.Write(ctx, func(tx *sql.Tx) error {
		return s.options.AuditLog.RecordInTx(ctx, tx, entry)
	})
}

func userEntry(actor auth.Session, request auth.RequestInfo, action string, details map[string]any) audit.Entry {
	userID := actor.UserID
	return audit.Entry{UserID: &userID, Username: actor.Username, Action: action, Outcome: audit.OutcomeSuccess, IP: request.IP, Details: details}
}

// CanFetch says a release channel is set.
func (s *Service) CanFetch() bool {
	return s.options.ReleaseURL != ""
}

// IsDevelopmentKey says this server trusts only the development release key.
func IsDevelopmentKey() bool {
	return release.IsDevelopmentBuild()
}

// Newest is the newest hosted build for a system, and the path of its file.
func (s *Service) Newest(osName, arch string) (Build, string, error) {
	build, err := s.buildFor(osName, arch, "")
	if err != nil {
		return Build{}, "", err
	}
	return build, filepath.Join(s.folder(), build.Version, build.File), nil
}
