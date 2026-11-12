// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/shared/buildinfo"
)

func TestAboutDescribesTheServerAndItsFleet(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	for range 2 {
		token := createToken(t, admin, api.CreateEnrollmentTokenRequest{ExpiresInHours: 1, MaxUses: 1})
		server.connectAgent(token.Secret, token.ServerFingerprint)
	}

	got := admin.browser.do(http.MethodGet, "/api/about", nil)
	expect(t, got, http.StatusOK, "")
	var about api.AboutResponse
	got.decode(t, &about)
	if about.ServerVersion != buildinfo.Version() || about.DatabaseBytes <= 0 || about.Machines != 2 {
		t.Errorf("about = %+v", about)
	}
	want := []api.AgentVersionCount{{Version: "v0.1.0", Machines: 2}}
	if !reflect.DeepEqual(about.AgentVersions, want) {
		t.Errorf("versions = %+v, want %+v", about.AgentVersions, want)
	}
	expect(t, server.newBrowser().do(http.MethodGet, "/api/about", nil), http.StatusUnauthorized, "not_authenticated")
}
