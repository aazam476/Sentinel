// SPDX-License-Identifier: Apache-2.0
import type {
  AuditEntry,
  PasskeySummary,
  SessionResponse,
  SessionSummary,
} from '../../../api/types.gen';
import { adminSession, fixtureNow } from '../normal/fixtures';

/**
 * Worst-case data for every screen that shows server data. Each value is
 * something real users produce, or the longest the server accepts:
 *
 * - usernames: 64 characters, letters, digits, . _ - only, so no spaces to
 *   wrap at (internal/server/auth/validation.go)
 * - passkey names: 64 characters of anything, including emoji and
 *   right-to-left text (internal/server/auth/passkeys.go)
 * - user agents: up to 512 bytes, often unrecognizable
 * - IP addresses: full-length IPv6
 * - audit usernames: whatever someone typed at the login form, capped by
 *   the server at 128 characters
 * - counts of exactly 1, empty lists and 1,000+ rows
 */

export const longUsername = 'bartholomew.fitzgerald-northwind-industries-holdings-sre-oncall01';

export const worstSession: SessionResponse = {
  ...adminSession,
  user: { id: 1, username: longUsername, role: 'admin' },
  secondFactors: { totp: true, passkey: true },
};

export const worstPasskeys: PasskeySummary[] = [
  {
    id: 1,
    name: 'Aleksandra’s MacBook Pro 16-inch (2026) – Nordic office 🔑🔑',
    createdAt: fixtureNow,
    lastUsedAt: null,
  },
  {
    id: 2,
    name: 'مفتاح الأمان الخاص بالعمل في مكتب الرياض الرئيسي',
    createdAt: fixtureNow - 1,
    lastUsedAt: fixtureNow,
  },
  { id: 3, name: 'J', createdAt: fixtureNow - 2, lastUsedAt: null },
  {
    id: 4,
    name: 'YubiKey5NFC-serial-27183920-backup-kept-in-the-safe-deposit-box1',
    createdAt: 0,
    lastUsedAt: 0,
  },
];

export const worstSessions: SessionSummary[] = [
  {
    id: 1,
    createdAt: fixtureNow,
    lastSeenAt: fixtureNow,
    ip: '2001:0db8:85a3:0000:0000:8a2e:0370:7334',
    userAgent: 'curl/8.11.0',
    current: true,
  },
  {
    id: 2,
    createdAt: 0,
    lastSeenAt: 0,
    ip: '::ffff:198.51.100.200',
    userAgent: 'x'.repeat(512),
    current: false,
  },
  {
    id: 3,
    createdAt: fixtureNow,
    lastSeenAt: fixtureNow,
    ip: '',
    userAgent: '',
    current: false,
  },
];

/** worstAuditEntries has 1,200 entries, the first few covering every extreme. */
export function worstAuditEntries(): AuditEntry[] {
  const emptiest: AuditEntry = {
    id: 1198,
    time: 0,
    userId: null,
    username: '',
    agentId: null,
    action: 'x',
    details: {},
    outcome: 'started',
    ip: '',
    previousHash: 'd'.repeat(64),
    hash: 'c'.repeat(64),
  };
  const extremes: AuditEntry[] = [
    {
      id: 1200,
      time: fixtureNow,
      userId: null,
      username: `admin' OR '1'='1 -- ${'a'.repeat(110)}`.slice(0, 128),
      agentId: null,
      action: 'auth.login.password',
      details: { reason: 'invalid_credentials' },
      outcome: 'failure',
      ip: '2001:0db8:85a3:0000:0000:8a2e:0370:7334',
      previousHash: 'f'.repeat(64),
      hash: 'e'.repeat(64),
    },
    {
      id: 1199,
      time: fixtureNow - 1,
      userId: 1,
      username: longUsername,
      agentId: 9_007_199_254_740_991,
      action: 'docker.container.restart.with.a.very.long.dotted.action.name.from.a.future.feature',
      details: {
        userId: 2,
        username: 'ünïcødé-用户-👩‍💻',
        nested: { from: 'viewer', to: 'admin' },
        list: [1, 2, 3],
      },
      outcome: 'denied',
      ip: '198.51.100.1',
      previousHash: 'e'.repeat(64),
      hash: 'd'.repeat(64),
    },
    emptiest,
  ];
  const ordinary: AuditEntry[] = Array.from({ length: 1197 }, (_, index) => ({
    ...emptiest,
    id: 1197 - index,
    time: fixtureNow - index * 1000,
    username: 'admin',
    action: 'auth.login',
    outcome: 'success',
    ip: '203.0.113.7',
    details: { method: 'totp' },
  }));
  return [...extremes, ...ordinary];
}
