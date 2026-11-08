// SPDX-License-Identifier: Apache-2.0

package docker_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/docker"
)

// fixtures holds the Engine API answers captured from Docker 29.9.
const fixtures = "testdata/engine-29.9/"

// Container IDs in the fixtures.
const (
	webID = "cfbe676c88bc"
	ttyID = "1655239731de"
)

// fakeEngine is a Docker Engine API that answers from captured responses
// and records every request that changes something.
type fakeEngine struct {
	t      *testing.T
	server *httptest.Server
	mutex  sync.Mutex
	calls  []string
	// failures maps "METHOD /path" (version prefix removed) to a status
	// and Docker's error message.
	failures map[string]failure
	// pullFixture is the progress stream a pull answers with.
	pullFixture string
}

type failure struct {
	status  int
	message string
}

// versionPrefix is the /v1.xx the SDK puts before every path.
var versionPrefix = regexp.MustCompile(`^/v[0-9.]+`)

func newFakeEngine(t *testing.T) *fakeEngine {
	t.Helper()
	fake := &fakeEngine{t: t, failures: map[string]failure{}, pullFixture: "pull.jsonl"}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

// engine returns an Engine pointed at the fake.
func (f *fakeEngine) engine() *docker.Engine {
	return docker.New("tcp://"+strings.TrimPrefix(f.server.URL, "http://"), "linux")
}

func (f *fakeEngine) fail(request string, status int, message string) {
	f.failures[request] = failure{status, message}
}

func (f *fakeEngine) recorded() []string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeEngine) serve(w http.ResponseWriter, r *http.Request) {
	path := versionPrefix.ReplaceAllString(r.URL.Path, "")
	request := r.Method + " " + path
	w.Header().Set("Api-Version", "1.56")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		f.mutex.Lock()
		f.calls = append(f.calls, request+queryOf(r))
		f.mutex.Unlock()
	}
	if failed, ok := f.failures[request]; ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(failed.status)
		_, _ = w.Write([]byte(`{"message":"` + failed.message + `"}`)) // a failed write fails the test that reads it
		return
	}
	file, status := f.route(r.Method, path)
	if file == "" {
		w.WriteHeader(status)
		return
	}
	data, err := os.ReadFile(fixtures + file)
	if err != nil {
		f.t.Errorf("fixture %s: %v", file, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if strings.HasSuffix(file, ".json") {
		w.Header().Set("Content-Type", "application/json")
	}
	_, _ = w.Write(data) // a failed write fails the test that reads it
}

func queryOf(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return ""
	}
	return "?" + r.URL.RawQuery
}

// route picks the fixture for a request, or a bare status.
func (f *fakeEngine) route(method, path string) (string, int) {
	switch {
	case path == "/_ping":
		return "", http.StatusOK
	case path == "/version":
		return "version.json", 0
	case path == "/containers/json":
		return "containers.json", 0
	case strings.HasSuffix(path, "/stats"):
		return "stats-web.json", 0
	case path == "/containers/"+ttyID+"/json":
		return "inspect-tty.json", 0
	case strings.HasPrefix(path, "/containers/") && strings.HasSuffix(path, "/json"):
		return "inspect-web.json", 0
	case path == "/containers/"+ttyID+"/logs":
		return "logs-tty.bin", 0
	case strings.HasSuffix(path, "/logs"):
		return "logs-web.bin", 0
	case path == "/images/create":
		return f.pullFixture, 0
	case strings.HasPrefix(path, "/distribution/"):
		return "distribution.json", 0
	case path == "/images/json":
		return "images.json", 0
	case strings.HasPrefix(path, "/images/") && strings.HasSuffix(path, "/json"):
		return "image-inspect.json", 0
	case path == "/volumes":
		return "volumes.json", 0
	case path == "/networks":
		return "networks.json", 0
	case method == http.MethodDelete && strings.HasPrefix(path, "/images/"):
		return "image-remove.json", 0
	case method == http.MethodDelete:
		return "", http.StatusNoContent
	default:
		return "", http.StatusNoContent // start, stop, restart
	}
}
