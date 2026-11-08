// SPDX-License-Identifier: Apache-2.0
import { countOf, describeUserAgent, formatDateTime } from './formatting';

describe('countOf', () => {
  it.each([
    [0, '0 entries'],
    [1, '1 entry'],
    [2, '2 entries'],
    [1284, '1,284 entries'],
  ])('writes %d as %j', (count, expected) => {
    expect(countOf(count, 'entry', 'entries', 'en-US')).toBe(expected);
  });
});

describe('formatDateTime', () => {
  it('shows date and time in the given locale', () => {
    const text = formatDateTime(Date.UTC(2026, 10, 7, 12, 0), 'en-US');
    expect(text).toMatch(/Nov 7, 2026/);
  });
});

describe('describeUserAgent', () => {
  it.each([
    [
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 15_1) AppleWebKit/605.1.15 Version/18.1 Safari/605.1.15',
      'Safari on macOS',
    ],
    ['Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0 Safari/537.36', 'Chrome on Windows'],
    ['Mozilla/5.0 (Windows NT 10.0) Chrome/131.0 Safari/537.36 Edg/131.0', 'Edge on Windows'],
    ['Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0', 'Firefox on Linux'],
    [
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) Version/18.1 Safari/604.1',
      'Safari on iOS',
    ],
    ['Mozilla/5.0 (Linux; Android 15) Chrome/131.0 Mobile Safari/537.36', 'Chrome on Android'],
    ['curl/8.11.0', 'Unknown browser'],
    ['', 'Unknown browser'],
    ['SomeBot (Linux)', 'Linux'],
    ['Firefox/133.0', 'Firefox'],
  ])('describes %j as %j', (userAgent, expected) => {
    expect(describeUserAgent(userAgent)).toBe(expected);
  });
});
