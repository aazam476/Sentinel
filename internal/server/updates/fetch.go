// SPDX-License-Identifier: Apache-2.0

package updates

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/shared/release"
)

// Fetch downloads the agent builds of the latest release from the release
// channel, checking the signed checksum list and each build's own
// signature (SEC-31), and hosts them. It returns what it added.
func (s *Service) Fetch(ctx context.Context, actor auth.Session, request auth.RequestInfo) ([]Build, error) {
	if err := auth.Authorize(actor, auth.PermissionManageMachines); err != nil {
		return nil, err
	}
	if s.options.ReleaseURL == "" {
		return nil, &InvalidError{"releaseUrl", "no release channel is set; upload builds instead"}
	}
	added, err := s.fetch(ctx)
	if err != nil {
		var invalid *InvalidError
		if !errors.As(err, &invalid) {
			err = &FetchError{Err: err}
		}
		return added, err
	}
	return added, s.audit(ctx, userEntry(actor, request, "agent.build.fetch", map[string]any{"builds": len(added)}))
}

// FetchError says the release channel couldn't be reached, or what it
// served didn't check out.
type FetchError struct {
	Err error
}

func (e *FetchError) Error() string { return e.Err.Error() }

func (e *FetchError) Unwrap() error { return e.Err }

func (s *Service) fetch(ctx context.Context) ([]Build, error) {
	base, err := s.releaseBase(ctx)
	if err != nil {
		return nil, err
	}
	sums, err := s.download(ctx, base, "SHA256SUMS")
	if err != nil {
		return nil, err
	}
	sumsSignature, err := s.download(ctx, base, "SHA256SUMS.sig")
	if err != nil {
		return nil, err
	}
	if _, err := release.VerifyWith(bytes.NewReader(sums), sumsSignature, release.Expect{Name: "SHA256SUMS"}, s.options.Keys); err != nil {
		return nil, fmt.Errorf("the release's checksum list was refused: %w", err)
	}
	added := []Build{}
	for name, digest := range agentBuilds(sums) {
		build, err := s.fetchBuild(ctx, base, name, digest)
		if err != nil {
			return added, err
		}
		added = append(added, build)
	}
	return added, nil
}

// agentBuilds reads the agent binaries a SHA256SUMS file lists.
func agentBuilds(sums []byte) map[string]string {
	builds := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		digest, name, found := strings.Cut(scanner.Text(), "  ")
		name = strings.TrimPrefix(name, "./")
		if !found || !strings.HasPrefix(name, "sentinel-agent_") || strings.HasSuffix(name, ".sig") || strings.HasSuffix(name, ".json") ||
			!safeSegment.MatchString(name) {
			continue
		}
		builds[name] = digest
	}
	return builds
}

func (s *Service) fetchBuild(ctx context.Context, base, name, digest string) (Build, error) {
	content, err := s.download(ctx, base, name)
	if err != nil {
		return Build{}, err
	}
	actual := sha256.Sum256(content)
	if hex.EncodeToString(actual[:]) != digest {
		return Build{}, fmt.Errorf("%s doesn't match the release's checksum", name)
	}
	signature, err := s.download(ctx, base, name+".sig")
	if err != nil {
		return Build{}, err
	}
	return s.add(bytes.NewReader(content), signature)
}

func (s *Service) download(ctx context.Context, releaseBase, name string) ([]byte, error) {
	base, err := url.Parse(releaseBase)
	if err != nil {
		return nil, &InvalidError{"releaseUrl", "the release channel's address is broken"}
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, base.JoinPath(name).String(), nil) // a parsed URL is always valid
	response, err := s.options.HTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: the release channel answered %s", name, response.Status)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxBuildSize+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	if len(content) > maxBuildSize {
		return nil, errors.New(name + " is larger than any release file")
	}
	return content, nil
}
