// SPDX-License-Identifier: Apache-2.0
import { http, HttpResponse } from 'msw';

import { apiError, apiPath, server } from '../test/server';
import { ApiError, apiRequest, apiURL, isApiError } from './client';

describe('apiRequest', () => {
  it('returns the parsed JSON body', async () => {
    server.use(http.get(apiPath('thing'), () => HttpResponse.json({ answer: 42 })));
    await expect(apiRequest<{ answer: number }>('api/thing')).resolves.toEqual({ answer: 42 });
  });

  it('sends a JSON body', async () => {
    server.use(
      http.post(apiPath('echo'), async ({ request }) => HttpResponse.json(await request.json())),
    );
    await expect(apiRequest('api/echo', { method: 'POST', body: { name: 'x' } })).resolves.toEqual({
      name: 'x',
    });
  });

  it('returns undefined for an empty response', async () => {
    server.use(http.delete(apiPath('thing'), () => new HttpResponse(null, { status: 204 })));
    await expect(apiRequest('api/thing', { method: 'DELETE' })).resolves.toBeUndefined();
  });

  it('throws an ApiError with the server error code', async () => {
    server.use(
      http.get(apiPath('thing'), () => apiError(403, 'forbidden', 'no', { field: 'role' })),
    );
    const error = await apiRequest('api/thing').catch((caught: unknown) => caught);
    expect(isApiError(error, 'forbidden')).toBe(true);
    expect(error).toMatchObject({ status: 403, field: 'role', message: 'no' });
  });

  it('throws an ApiError for a non-JSON error page', async () => {
    server.use(
      http.get(
        apiPath('thing'),
        () => new HttpResponse('<html>Bad gateway</html>', { status: 502 }),
      ),
    );
    const error = await apiRequest('api/thing').catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      code: 'unknown',
      message: 'The server answered with status 502.',
    });
  });

  it('resolves paths against the page base', () => {
    expect(apiURL('api/x')).toBe(new URL('api/x', document.baseURI).toString());
  });

  it('isApiError rejects other values', () => {
    expect(isApiError(new Error('x'))).toBe(false);
    expect(isApiError(new ApiError(400, undefined), 'other')).toBe(false);
  });
});
