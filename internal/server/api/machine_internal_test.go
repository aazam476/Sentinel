// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

func TestWriteAgentError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"an offline machine", hub.ErrOffline, http.StatusConflict, errorCodeMachineOffline},
		{"a machine that dropped while answering", hub.ErrDisconnected, http.StatusConflict, errorCodeMachineOffline},
		{"a machine that didn't answer in time", hub.ErrTimeout, http.StatusGatewayTimeout, errorCodeMachineTimeout},
		{"a local policy refusal", &protocol.Error{Code: protocol.ErrorPolicyDenied}, http.StatusForbidden, errorCodePolicyDenied},
		{"something the machine doesn't have", &protocol.Error{Code: protocol.ErrorNotFound}, http.StatusNotFound, errorCodeNotFound},
		{"a request the agent found invalid", &protocol.Error{Code: protocol.ErrorInvalidRequest}, http.StatusBadRequest, errorCodeInvalidInput},
		{"a system the agent can't do it on", &protocol.Error{Code: protocol.ErrorUnsupportedPlatform}, http.StatusNotImplemented, errorCodeUnsupported},
		{"an agent too old to know the action", &protocol.Error{Code: protocol.ErrorUnknownAction}, http.StatusNotImplemented, errorCodeUnsupported},
		{"a state Docker refuses in", &protocol.Error{Code: protocol.ErrorConflict}, http.StatusConflict, errorCodeConflict},
		{"a machine without Docker", &protocol.Error{Code: protocol.ErrorDockerUnavailable}, http.StatusServiceUnavailable, errorCodeNoDocker},
		{"a failure on the machine", &protocol.Error{Code: protocol.ErrorInternal, Message: "systemctl: exit status 1"}, http.StatusBadGateway, errorCodeAgentFailed},
		{"a failure on the server", errors.New("encode: broken"), http.StatusInternalServerError, errorCodeInternal},
	}
	h := &handlers{logger: slog.New(slog.DiscardHandler)}
	for _, c := range cases {
		t.Run("reports "+c.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			h.writeAgentError(recorder, c.err)
			if recorder.Code != c.status || !strings.Contains(recorder.Body.String(), `"code":"`+c.code+`"`) {
				t.Errorf("got %d %s", recorder.Code, recorder.Body)
			}
		})
	}
}
