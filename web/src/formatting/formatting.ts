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

/** formatNumber writes a whole number with the locale's separators: "1,284". */
export function formatNumber(value: number, locale?: string): string {
  return new Intl.NumberFormat(locale).format(value);
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

/** formatPercent shows a 0–100 value as a whole percentage, e.g. "42%". */
export function formatPercent(value: number, locale?: string): string {
  return new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0 }).format(
    value / 100,
  );
}

const byteUnits = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

/**
 * formatBytes shows a size in the largest unit that keeps it at or above
 * 1, in powers of 1,024 (as operating systems report memory and disks):
 * "512 B", "8 GB", "1.5 TB".
 */
export function formatBytes(bytes: number, locale?: string): string {
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 1024 && unit < byteUnits.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const digits = value < 10 && unit > 0 ? 1 : 0;
  const number = new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(value);
  return `${number} ${byteUnits[unit] ?? 'B'}`;
}

/** formatRate shows bytes per second, e.g. "1.2 MB/s". */
export function formatRate(bytesPerSecond: number, locale?: string): string {
  return `${formatBytes(bytesPerSecond, locale)}/s`;
}

/** formatUptime shows a duration in its two largest units: "3 d 4 h", "12 min". */
export function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3_600);
  const minutes = Math.floor((seconds % 3_600) / 60);
  if (days > 0) {
    return `${String(days)} d ${String(hours)} h`;
  }
  if (hours > 0) {
    return `${String(hours)} h ${String(minutes)} min`;
  }
  return `${String(minutes)} min`;
}

/**
 * formatRelative shows how long ago a time was, e.g. "5 minutes ago", for
 * "last seen" labels. now is a parameter so tests are deterministic.
 */
export function formatRelative(milliseconds: number, now: number, locale?: string): string {
  const seconds = Math.round((milliseconds - now) / 1000);
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['day', 86_400],
    ['hour', 3_600],
    ['minute', 60],
  ];
  const format = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return format.format(Math.round(seconds / size), unit);
    }
  }
  return format.format(0, 'second');
}
