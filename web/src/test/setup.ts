// SPDX-License-Identifier: Apache-2.0
import '@testing-library/jest-dom/vitest';
import { cleanup, configure } from '@testing-library/react';
import { createElement } from 'react';
import { afterAll, afterEach, beforeAll, vi } from 'vitest';

import { toast } from 'sonner';

import { server } from './server';

// Pages load lazily, and the machine page brings the chart libraries; on a
// slow CI runner the first test to open one can take over the default 1 s
// to find its content. This is only an upper bound: passing tests don't
// wait longer.
configure({ asyncUtilTimeout: 5_000 });

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

// Tests that stand in for browser APIs (EventSource) do so for themselves.
afterEach(() => {
  vi.unstubAllGlobals();
});

// jsdom has no canvas, which Liveline draws on. Tests check what the live
// chart is given rather than how it is drawn; Playwright checks the real
// chart in a browser.
vi.mock('liveline', () => ({
  Liveline: ({ data, loading }: { data: { time: number; value: number }[]; loading?: boolean }) =>
    createElement('div', {
      'data-testid': 'live-chart',
      'data-points': data.map((point) => point.value).join(','),
      'data-loading': String(loading === true),
    }),
}));

// Recharts sizes charts with ResizeObserver, which jsdom doesn't have.
class FixedSizeObserver implements ResizeObserver {
  constructor(private readonly callback: ResizeObserverCallback) {}

  observe(target: Element) {
    const entry = { target, contentRect: { width: 600, height: 240 } } as ResizeObserverEntry;
    this.callback([entry], this);
  }

  unobserve() {}

  disconnect() {}
}
globalThis.ResizeObserver = FixedSizeObserver;
