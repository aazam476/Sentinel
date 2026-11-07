// SPDX-License-Identifier: Apache-2.0

package httpserver_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/aazam476/sentinel/internal/server/httpserver"
)

func TestServe(t *testing.T) {
	t.Run("serves requests with the given handler", func(t *testing.T) {
		listener := listenOnLoopback(t)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "hello from the handler") // a failed write only affects this test request
		})
		stop := startServing(t, listener, handler)
		defer stop()

		body := getBody(t, "http://"+listener.Addr().String()+"/")

		if body != "hello from the handler" {
			t.Errorf("response body = %q, want %q", body, "hello from the handler")
		}
	})

	t.Run("returns nil after a graceful shutdown", func(t *testing.T) {
		listener := listenOnLoopback(t)
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { result <- httpserver.Serve(ctx, listener, http.NotFoundHandler()) }()

		cancel()

		if err := <-result; err != nil {
			t.Errorf("Serve returned %v after shutdown, want nil", err)
		}
	})

	t.Run("lets an in-flight request finish during shutdown", func(t *testing.T) {
		listener := listenOnLoopback(t)
		requestStarted := make(chan struct{})
		releaseRequest := make(chan struct{})
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(requestStarted)
			<-releaseRequest
			_, _ = io.WriteString(w, "finished") // a failed write fails the assertion below
		})
		ctx, cancel := context.WithCancel(context.Background())
		serveResult := make(chan error, 1)
		go func() { serveResult <- httpserver.Serve(ctx, listener, handler) }()

		bodyResult := make(chan string, 1)
		go func() { bodyResult <- getBody(t, "http://"+listener.Addr().String()+"/") }()
		<-requestStarted
		cancel()
		close(releaseRequest)

		if body := <-bodyResult; body != "finished" {
			t.Errorf("in-flight response body = %q, want %q", body, "finished")
		}
		if err := <-serveResult; err != nil {
			t.Errorf("Serve returned %v, want nil", err)
		}
	})

	t.Run("returns an error when the listener is already closed", func(t *testing.T) {
		listener := listenOnLoopback(t)
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}

		err := httpserver.Serve(context.Background(), listener, http.NotFoundHandler())

		if err == nil {
			t.Fatal("Serve returned nil for a closed listener, want an error")
		}
	})
}

func listenOnLoopback(t *testing.T) net.Listener {
	t.Helper()
	listener, err := httpserver.Listen(t.Context(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return listener
}

// startServing runs Serve in the background and returns a function that
// stops it and checks it shut down cleanly.
func startServing(t *testing.T, listener net.Listener, handler http.Handler) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- httpserver.Serve(ctx, listener, handler) }()

	return func() {
		cancel()
		if err := <-result; err != nil {
			t.Errorf("Serve returned %v, want nil", err)
		}
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Errorf("build request for %s: %v", url, err)
		return ""
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Errorf("GET %s: %v", url, err)
		return ""
	}
	defer func() { _ = response.Body.Close() }() // nothing useful to do if closing fails in a test

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Errorf("read body of %s: %v", url, err)
		return ""
	}
	return string(body)
}
