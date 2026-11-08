// SPDX-License-Identifier: Apache-2.0

/**
 * Formatting helpers for values from the server. Times arrive as UTC Unix
 * milliseconds and are shown in the browser's time zone and language.
 */

/** formatDateTime shows a timestamp as a date and time, e.g. "Nov 7, 2026, 12:04". */
export function formatDateTime(milliseconds: number, locale?: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(
    milliseconds,
  );
}

/**
 * countOf writes a count with the right noun form, e.g. "1 entry" and
 * "2 entries", so the UI never says "1 entries".
 */
export function countOf(count: number, singular: string, plural: string, locale?: string): string {
  const form = new Intl.PluralRules(locale).select(count) === 'one' ? singular : plural;
  return `${new Intl.NumberFormat(locale).format(count)} ${form}`;
}

/**
 * describeUserAgent turns a browser's User-Agent header into "Browser on
 * OS", good enough to tell sessions apart. Unknown parts are left out.
 */
export function describeUserAgent(userAgent: string): string {
  const browser = firstMatch(userAgent, [
    [/Edg\//, 'Edge'],
    [/Firefox\//, 'Firefox'],
    [/Chrome\//, 'Chrome'],
    [/Safari\//, 'Safari'],
  ]);
  const system = firstMatch(userAgent, [
    [/iPhone|iPad/, 'iOS'],
    [/Android/, 'Android'],
    [/Windows/, 'Windows'],
    [/Mac OS X|Macintosh/, 'macOS'],
    [/Linux/, 'Linux'],
  ]);
  if (browser !== undefined && system !== undefined) {
    return `${browser} on ${system}`;
  }
  return browser ?? system ?? 'Unknown browser';
}

function firstMatch(value: string, patterns: [RegExp, string][]): string | undefined {
  return patterns.find(([pattern]) => pattern.test(value))?.[1];
}
