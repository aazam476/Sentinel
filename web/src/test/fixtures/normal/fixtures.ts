// SPDX-License-Identifier: Apache-2.0
import type {
  AuditEntry,
  PasskeySummary,
  SessionResponse,
  SessionSummary,
} from '../../../api/types.gen';

/** The fixed "now" every fixture is relative to: 2026-11-07 12:00 UTC. */
export const fixtureNow = Date.UTC(2026, 10, 7, 12, 0, 0);

export const adminSession: SessionResponse = {
  state: 'signed_in',
  user: { id: 1, username: 'admin', role: 'admin' },
  stepUpValidUntil: fixtureNow + 15 * 60_000,
  secondFactors: { totp: true, passkey: false },
};

export const viewerSession: SessionResponse = {
  ...adminSession,
  user: { id: 2, username: 'vic', role: 'viewer' },
};

export const pendingSession: SessionResponse = {
  state: 'second_factor',
  user: { id: 1, username: 'admin', role: 'admin' },
  stepUpValidUntil: null,
  secondFactors: { totp: true, passkey: true },
};

export const enrollingSession: SessionResponse = {
  state: 'enroll_second_factor',
  user: { id: 3, username: 'newcomer', role: 'operator' },
  stepUpValidUntil: null,
  secondFactors: { totp: false, passkey: false },
};

export const sessions: SessionSummary[] = [
  {
    id: 10,
    createdAt: fixtureNow - 3_600_000,
    lastSeenAt: fixtureNow,
    ip: '203.0.113.7',
    userAgent:
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 15_1) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15',
    current: true,
  },
  {
    id: 11,
    createdAt: fixtureNow - 86_400_000,
    lastSeenAt: fixtureNow - 7_200_000,
    ip: '198.51.100.20',
    userAgent:
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36',
    current: false,
  },
];

export const passkeys: PasskeySummary[] = [
  { id: 1, name: 'Laptop', createdAt: fixtureNow - 86_400_000, lastUsedAt: fixtureNow - 3_600_000 },
];

export const auditEntries: AuditEntry[] = [
  {
    id: 2,
    time: fixtureNow,
    userId: 1,
    username: 'admin',
    agentId: null,
    action: 'auth.login',
    details: { method: 'totp' },
    outcome: 'success',
    ip: '203.0.113.7',
    previousHash: 'a'.repeat(64),
    hash: 'b'.repeat(64),
  },
  {
    id: 1,
    time: fixtureNow - 60_000,
    userId: null,
    username: 'mallory',
    agentId: null,
    action: 'auth.login.password',
    details: { reason: 'invalid_credentials' },
    outcome: 'failure',
    ip: '198.51.100.1',
    previousHash: '0'.repeat(64),
    hash: 'a'.repeat(64),
  },
];
