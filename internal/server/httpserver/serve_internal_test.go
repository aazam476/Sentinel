// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"context"
	"net"
	"net/http"
	"testing"
)

func TestShutdown(t *testing.T) {
	t.Run("returns an error when requests don't finish before the timeout", func(t *testing.T) {
		var config net.ListenConfig
		listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		requestStarted := make(chan struct{})
		releaseRequest := make(chan struct{})
		defer close(releaseRequest)
		server := &http.Server{
			ReadHeaderTimeout: readHeaderTimeout,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(requestStarted)
				<-releaseRequest
			}),
		}
		go func() { _ = server.Serve(listener) }() // ends when the server is shut down below
		go func() {
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+listener.Addr().String()+"/", nil)
			response, err := http.DefaultClient.Do(request)
			if err == nil {
				_ = response.Body.Close() // the response doesn't matter, only that the request was in flight
			}
		}()
		<-requestStarted

		err = shutdown(server, 0)

		if err == nil {
			t.Fatal("shutdown returned nil with a request still running, want a timeout error")
		}
	})
}
