// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"

	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/app"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// strongPassword passes the password policy.
const strongPassword = "correct horse battery staple"

// testServer is the whole server stack, started in-process over TLS with a
// temporary data directory and a fake clock. Tests reach it only over HTTP.
//
// Its public URL is https://localhost:<port>, because passkeys need a host
// name rather than an IP address. Requests still go to 127.0.0.1, and carry
// the public URL as their Origin, like a browser at that address.
type testServer struct {
	// releaseFiles are the files on the server's release channel, by name.
	releaseFiles *sync.Map
	t            *testing.T
	server       *httptest.Server
	app          *app.App
	clock        *fakeclock.Clock
	publicOrigin string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	httpServer := httptest.NewUnstartedServer(nil)
	publicOrigin := "https://localhost:" + portOf(t, httpServer.Listener.Addr().String())
	// A release channel on this machine: tests never reach the internet.
	releaseFiles := &sync.Map{}
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, found := releaseFiles.Load(strings.TrimPrefix(r.URL.Path, "/"))
		if !found {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(content.([]byte)) //nolint:gosec // G705: release files a test stored, served as bytes
	}))
	t.Cleanup(releases.Close)
	settings, err := config.File{
		DataDir: t.TempDir(), Listen: "127.0.0.1:0", PublicURL: publicOrigin,
		Updates: config.UpdatesFile{ReleaseURL: releases.URL + "/"},
		TURN:    config.TURNFile{Listen: "127.0.0.1:0", RelayIP: "127.0.0.1"},
	}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	clock := fakeclock.New(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	webUI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "web ui") }) // a failed write fails the test that reads it
	server, err := app.New(t.Context(), app.Options{
		Settings: settings, Logger: slog.New(slog.DiscardHandler), Clock: clock, WebUI: webUI,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer.Config.Handler = server.Handler
	httpServer.StartTLS()
	t.Cleanup(func() {
		httpServer.Close()
		_ = server.Close() // nothing to check when a test database closes
	})
	return &testServer{t: t, server: httpServer, app: server, clock: clock, publicOrigin: publicOrigin, releaseFiles: releaseFiles}
}

func portOf(t *testing.T, address string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// browser is a client with its own cookie jar, like one browser profile.
type browser struct {
	t      *testing.T
	server *testServer
	client *http.Client
}

func (s *testServer) newBrowser() *browser {
	jar, err := cookiejar.New(nil)
	if err != nil {
		s.t.Fatal(err)
	}
	// The test server's Client is shared; each browser needs its own
	// cookie jar, so it gets its own client using the same TLS transport.
	client := &http.Client{Transport: s.server.Client().Transport, Jar: jar}
	return &browser{t: s.t, server: s, client: client}
}

// response is a finished HTTP exchange.
type response struct {
	status int
	header http.Header
	body   []byte
}

// errorCode returns the {"error": {"code"}} of the response, or "".
func (r response) errorCode() string {
	var body api.ErrorResponse
	_ = json.Unmarshal(r.body, &body) // a body that isn't an error simply has no code
	return body.Error.Code
}

func (r response) decode(t *testing.T, destination any) {
	t.Helper()
	if err := json.Unmarshal(r.body, destination); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

// do sends a request. Like a browser, it sends the page's Origin header on
// every request except GET.
func (b *browser) do(method, path string, body any) response {
	b.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			b.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(b.t.Context(), method, b.server.server.URL+path, reader)
	if err != nil {
		b.t.Fatal(err)
	}
	if method != http.MethodGet {
		request.Header.Set("Origin", b.server.publicOrigin)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return b.send(request)
}

// putRaw PUTs a body of raw bytes, as an upload piece is sent.
func (b *browser) putRaw(path string, body []byte) response {
	b.t.Helper()
	request, err := http.NewRequestWithContext(b.t.Context(), http.MethodPut, b.server.server.URL+path, bytes.NewReader(body))
	if err != nil {
		b.t.Fatal(err)
	}
	request.Header.Set("Origin", b.server.publicOrigin)
	request.Header.Set("Content-Type", "application/octet-stream")
	return b.send(request)
}

// doWith sends a request with extra headers, such as Range.
func (b *browser) doWith(method, path string, body any, headers map[string]string) response {
	b.t.Helper()
	if body != nil {
		b.t.Fatal("doWith sends no body")
	}
	request, err := http.NewRequestWithContext(b.t.Context(), method, b.server.server.URL+path, nil)
	if err != nil {
		b.t.Fatal(err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return b.send(request)
}

func (b *browser) send(request *http.Request) response {
	b.t.Helper()
	httpResponse, err := b.client.Do(request)
	if err != nil {
		b.t.Fatalf("%s %s: %v", request.Method, request.URL.Path, err)
	}
	defer func() { _ = httpResponse.Body.Close() }() // nothing to do if closing fails in a test
	body, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	return response{status: httpResponse.StatusCode, header: httpResponse.Header, body: body}
}

// expect fails the test unless the response has the status and, when
// given, the error code.
func expect(t *testing.T, got response, status int, code string) {
	t.Helper()
	if got.status != status || got.errorCode() != code {
		t.Fatalf("got %d %q (%s), want %d %q", got.status, got.errorCode(), strings.TrimSpace(string(got.body)), status, code)
	}
}

// admin holds what the tests need to act as the first admin.
type admin struct {
	browser       *browser
	totpSecret    string
	recoveryCodes []string
}

// setUpAdmin runs first-boot setup over HTTP and returns the signed-in
// admin's browser.
func (s *testServer) setUpAdmin() admin {
	s.t.Helper()
	browser := s.newBrowser()
	begun := browser.do(http.MethodPost, "/api/setup/begin", api.BeginSetupRequest{Username: "admin", Password: strongPassword})
	expect(s.t, begun, http.StatusOK, "")
	var setup api.BeginSetupResponse
	begun.decode(s.t, &setup)

	completed := browser.do(http.MethodPost, "/api/setup/complete", api.CompleteSetupRequest{
		SetupID: setup.SetupID, TOTPCode: s.code(setup.TOTP.Secret),
	})
	expect(s.t, completed, http.StatusOK, "")
	var signedIn api.SignedInResponse
	completed.decode(s.t, &signedIn)
	s.clock.Advance(30 * time.Second)
	return admin{browser: browser, totpSecret: setup.TOTP.Secret, recoveryCodes: signedIn.RecoveryCodes}
}

// addUser has the admin create a user and returns a browser signed in as
// them, after they enroll TOTP.
func (s *testServer) addUser(admin admin, username, role string) *browser {
	s.t.Helper()
	created := admin.browser.do(http.MethodPost, "/api/users", api.CreateUserRequest{Username: username, Password: strongPassword, Role: role})
	expect(s.t, created, http.StatusCreated, "")

	user := s.newBrowser()
	expect(s.t, user.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: username, Password: strongPassword}), http.StatusOK, "")
	started := user.do(http.MethodPost, "/api/auth/totp/enrollment", nil)
	expect(s.t, started, http.StatusOK, "")
	var enrollment api.TOTPEnrollmentResponse
	started.decode(s.t, &enrollment)
	expect(s.t, user.do(http.MethodPost, "/api/auth/totp/enrollment/confirm", api.TOTPCodeRequest{TOTPCode: s.code(enrollment.Secret)}), http.StatusOK, "")
	s.clock.Advance(30 * time.Second)
	return user
}

// code computes the TOTP code for secret at the fake clock's time.
func (s *testServer) code(secret string) string {
	step := uint64(s.clock.Now().Unix() / 30) //nolint:gosec // G115: test times are in 2026, far from overflow
	code, err := hotp.GenerateCodeCustom(secret, step, hotp.ValidateOpts{Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		s.t.Fatal(err)
	}
	return code
}
