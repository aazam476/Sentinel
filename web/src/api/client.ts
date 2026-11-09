// SPDX-License-Identifier: Apache-2.0
import type { ErrorResponse } from './types.gen';

/**
 * ApiError is a failed API request. `code` is the stable error code from
 * the server (see internal/server/api/errors.go), `message` a sentence for
 * people.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly field: string | undefined;
  readonly retryAfterSeconds: number | undefined;

  constructor(status: number, body: ErrorResponse | undefined) {
    super(body?.error.message ?? `The server answered with status ${String(status)}.`);
    this.name = 'ApiError';
    this.status = status;
    this.code = body?.error.code ?? 'unknown';
    this.field = body?.error.field;
    this.retryAfterSeconds = body?.error.retryAfterSeconds;
  }
}

/**
 * apiURL resolves an API path against the page's <base> element, which the
 * server sets to the configured base path, so the app works under any path.
 */
export function apiURL(path: string): string {
  return new URL(path, document.baseURI).toString();
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  /** body is sent as JSON. */
  body?: unknown;
  /** raw is sent as it is, as a file's piece is uploaded. */
  raw?: Blob;
  /** form is sent as multipart form data; the browser sets its type. */
  form?: FormData;
  signal?: AbortSignal;
}

/**
 * apiRequest calls the API and returns the parsed JSON body, or undefined
 * for an empty response. It throws ApiError for any non-2xx status. Only the
 * hooks in src/api call it; components use those hooks.
 */
export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  const init: RequestInit = {
    method: options.method ?? 'GET',
    credentials: 'same-origin',
    headers,
    signal: options.signal,
  };
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(options.body);
  }
  if (options.form !== undefined) {
    init.body = options.form;
  }
  if (options.raw !== undefined) {
    headers['Content-Type'] = 'application/octet-stream';
    init.body = options.raw;
  }

  const response = await fetch(apiURL(path), init);
  const parsed = parseBody(await response.text());
  if (!response.ok) {
    throw new ApiError(response.status, parsed as ErrorResponse | undefined);
  }
  return parsed as T;
}

/**
 * parseBody parses a JSON body, or returns undefined for an empty or
 * non-JSON one (a proxy's HTML error page, for example).
 */
function parseBody(text: string): unknown {
  if (text === '') {
    return undefined;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return undefined;
  }
}

/** isApiError narrows an unknown thrown value, optionally to one code. */
export function isApiError(error: unknown, code?: string): error is ApiError {
  return error instanceof ApiError && (code === undefined || error.code === code);
}
