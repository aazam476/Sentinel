// SPDX-License-Identifier: Apache-2.0
import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterAll, afterEach, beforeAll } from 'vitest';

import { toast } from 'sonner';

import { server } from './server';

// Any request without a mocked response fails the test, so a page can't
// quietly depend on an endpoint the test didn't think about.
beforeAll(() => {
  server.listen({ onUnhandledRequest: 'error' });
});

afterEach(() => {
  cleanup();
  server.resetHandlers();
});

afterAll(() => {
  server.close();
});

// Toasts live in Sonner's module-level store; clear them so one test's
// toast can't satisfy another test's assertion.
afterEach(() => {
  toast.dismiss();
});
