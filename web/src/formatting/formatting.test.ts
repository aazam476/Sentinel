// SPDX-License-Identifier: Apache-2.0
import {
  countOf,
  describeUserAgent,
  formatBytes,
  formatDateTime,
  formatPercent,
  formatRate,
  formatRelative,
  formatUptime,
} from './formatting';

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

describe('formatPercent', () => {
  it.each([
    [0, '0%'],
    [42.4, '42%'],
    [99.6, '100%'],
  ])('writes %d as %j', (value, expected) => {
    expect(formatPercent(value, 'en-US')).toBe(expected);
  });
});

describe('formatBytes and formatRate', () => {
  it.each([
    [0, '0 B'],
    [-5, '0 B'],
    [512, '512 B'],
    [1536, '1.5 KB'],
    [8 * 1024 ** 3, '8 GB'],
    [12.4 * 1024 ** 3, '12 GB'],
    [2 * 1024 ** 6, '2,048 PB'],
  ])('writes %d bytes as %j', (bytes, expected) => {
    expect(formatBytes(bytes, 'en-US')).toBe(expected);
  });

  it('adds per second to rates', () => {
    expect(formatRate(1_258_291, 'en-US')).toBe('1.2 MB/s');
  });
});

describe('formatUptime', () => {
  it.each([
    [59, '0 min'],
    [720, '12 min'],
    [3 * 3600 + 120, '3 h 2 min'],
    [3 * 86_400 + 4 * 3600 + 59, '3 d 4 h'],
  ])('writes %d seconds as %j', (seconds, expected) => {
    expect(formatUptime(seconds)).toBe(expected);
  });
});

describe('formatRelative', () => {
  const now = Date.UTC(2026, 10, 7, 12, 0);
  it.each([
    [now - 10_000, 'now'],
    [now - 5 * 60_000, '5 minutes ago'],
    [now - 3 * 3_600_000, '3 hours ago'],
    [now - 2 * 86_400_000, '2 days ago'],
  ])('writes %d as %j', (time, expected) => {
    expect(formatRelative(time, now, 'en-US')).toBe(expected);
  });
});
