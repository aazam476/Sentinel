// SPDX-License-Identifier: Apache-2.0

package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

// githubLatest matches a "latest release" download address on GitHub, or
// on a host laid out like it: <host>/<owner>/<repo>/releases/latest/download/.
var githubLatest = regexp.MustCompile(`^(https?://[^/]+)/([^/]+)/([^/]+)/releases/latest/download/?$`)

// maxReleasesListing bounds the releases list read from the API.
const maxReleasesListing = 1 << 20

// releaseBase is where to fetch builds from. GitHub's "latest" release
// is never a pre-release, so on the beta channel a "latest" address is
// resolved to the newest release that isn't a draft, pre-releases
// included.
func (s *Service) releaseBase(ctx context.Context) (string, error) {
	configured := s.options.ReleaseURL
	if s.channel(ctx) != "beta" {
		return configured, nil
	}
	match := githubLatest.FindStringSubmatch(configured)
	if match == nil {
		return configured, nil // another kind of channel: use it as it is
	}
	host, owner, repo := match[1], match[2], match[3]
	api := s.options.ReleasesAPIURL
	if api == "" {
		if host != "https://github.com" {
			return configured, nil
		}
		api = "https://api.github.com/repos/" + owner + "/" + repo + "/releases?per_page=20"
	}
	tag, err := s.newestRelease(ctx, api)
	if err != nil {
		return "", err
	}
	return host + "/" + owner + "/" + repo + "/releases/download/" + tag + "/", nil
}

func (s *Service) channel(ctx context.Context) string {
	if s.options.Channel == nil {
		return "stable"
	}
	return s.options.Channel(ctx)
}

// newestRelease is the tag of the first release the API lists that isn't
// a draft: GitHub lists the newest first.
func (s *Service) newestRelease(ctx context.Context, api string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return "", &InvalidError{"releaseUrl", "the release channel's address is broken"}
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := s.options.HTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("list releases: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list releases: the release channel answered %s", response.Status)
	}
	var releases []struct {
		Tag   string `json:"tag_name"`
		Draft bool   `json:"draft"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxReleasesListing)).Decode(&releases); err != nil {
		return "", fmt.Errorf("list releases: %w", err)
	}
	for _, release := range releases {
		if !release.Draft && safeSegment.MatchString(release.Tag) {
			return release.Tag, nil
		}
	}
	return "", errors.New("list releases: there are no releases yet")
}
