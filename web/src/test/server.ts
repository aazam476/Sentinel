// SPDX-License-Identifier: Apache-2.0
import { http, HttpResponse, type JsonBodyType } from 'msw';
import { setupServer } from 'msw/node';

import type { ErrorResponse, SessionResponse, SetupStatusResponse } from '../api/types.gen';

/**
 * server answers the web UI's API calls in component tests. Each test adds
 * the responses it needs with server.use(...). Response bodies use the
 * types generated from the Go API, so mocks can't drift from the server.
 */
export const server = setupServer();

/** apiPath matches an API path at whatever origin the test page has. */
export function apiPath(path: string): string {
  return `*/api/${path}`;
}

export function json(body: JsonBodyType, status = 200) {
  return HttpResponse.json(body, { status });
}

export function apiError(
  status: number,
  code: string,
  message: string,
  extra: Partial<ErrorResponse['error']> = {},
) {
  const body: ErrorResponse = { error: { code, message, ...extra } };
  return HttpResponse.json(body, { status });
}

/**
 * signedInAs sets up the requests every page makes first: setup status and
 * the current session (null: not signed in), plus an empty fleet for the
 * home page.
 */
export function signedInAs(session: SessionResponse | null, setupRequired = false) {
  const status: SetupStatusResponse = { setupRequired };
  server.use(
    http.get(apiPath('setup/status'), () => json(status)),
    http.get(apiPath('auth/session'), () =>
      session === null
        ? apiError(401, 'not_authenticated', 'you are not signed in')
        : json(session),
    ),
    // The home page lists machines; tests that care add their own list.
    http.get(apiPath('agents'), () => json({ agents: [] })),
    // 2FA is optional unless a test says otherwise (ADR 0039).
    http.get(apiPath('settings/security'), () => json({ requireTwoFactor: false })),
  );
}
