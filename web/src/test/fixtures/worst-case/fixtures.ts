// SPDX-License-Identifier: Apache-2.0
import type {
  Agent,
  AgentDetails,
  AuditEntry,
  EnrollmentToken,
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
 * - machines (internal/server/fleet/validation.go): display names up to 64
 *   characters, hostnames up to 253, tags up to 32 (20 per machine), other
 *   reported text up to 128; readings from 0 to 100% and missing ones
 * - filesystems: mount points up to 512 bytes, up to 256 of them
 *   (internal/server/metrics/ingest.go)
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

/** A 253-character FQDN: the longest hostname DNS allows. */
export const longHostname = [
  'storage-array-controller-primary-rack42-unit07',
  'datacenter-frankfurt-west-availability-zone-c',
  'northwind-industries-holdings-infrastructure',
  'internal-monitoring-and-observability-platform',
  'example-corporate-network-segment.example',
]
  .join('.')
  .slice(0, 253);

/** The longest display name: 64 characters with accents and an em dash. */
export const longDisplayName = 'Zentraler Datenspeicher — Wohnzimmer, Kellerregal 3, Fach unten!';

const maxTags = Array.from({ length: 20 }, (_, index) =>
  `environment-production-eu-${String(index).padStart(2, '0')}`.slice(0, 32),
);

const base: Agent = {
  id: '01JBX3K5Q7Z8M4N6P8R0T2V4W0',
  name: longDisplayName,
  displayName: longDisplayName,
  hostname: longHostname,
  os: 'linux',
  platform: 'Red Hat Enterprise Linux Server for SAP Applications with High Availability',
  platformVersion: '9.4 (Plow) Extended Update Support Long Term Maintenance Release',
  arch: 'aarch64_be',
  agentVersion: 'v0.1.0-beta.1+build.2026.11.08.abcdef1234567890',
  online: true,
  lastSeenAt: fixtureNow,
  enrolledAt: fixtureNow - 400 * 86_400_000,
  tags: maxTags,
  current: {
    time: fixtureNow,
    uptimeSeconds: 3_650 * 86_400,
    values: { 'cpu.percent': 100, 'memory.percent': 0, 'disk.percent': 99.9 },
  },
};

/**
 * worstAgents is 1,200 machines; the first few are the hardest cases, the
 * rest plain, as a large fleet would be.
 */
export function worstAgents(): Agent[] {
  const hard: Agent[] = [
    base,
    {
      ...base,
      id: '01JBX3K5Q7Z8M4N6P8R0T2V4W1',
      name: 'خادم-الملفات-الرئيسي',
      displayName: 'خادم-الملفات-الرئيسي',
      tags: ['مكتب'],
    },
    {
      ...base,
      id: '01JBX3K5Q7Z8M4N6P8R0T2V4W2',
      name: 'x',
      displayName: '',
      hostname: 'x',
      tags: [],
      platform: '',
      platformVersion: '',
    },
    {
      ...base,
      id: '01JBX3K5Q7Z8M4N6P8R0T2V4W3',
      online: false,
      lastSeenAt: null,
      current: null,
      name: 'never-connected',
      displayName: '',
      hostname: 'never-connected',
    },
    {
      ...base,
      id: '01JBX3K5Q7Z8M4N6P8R0T2V4W4',
      online: true,
      current: null,
      name: 'just-enrolled',
      displayName: '',
    },
    {
      ...base,
      id: '01JBX3K5Q7Z8M4N6P8R0T2V4W5',
      current: { time: fixtureNow, uptimeSeconds: 0, values: {} },
    },
    {
      ...base,
      id: '01JBX3K5Q7Z8M4N6P8R0T2V4W6',
      online: false,
      current: null,
      lastSeenAt: fixtureNow - 800 * 86_400_000,
    },
  ];
  const plain = Array.from({ length: 1_200 - hard.length }, (_, index) => ({
    ...base,
    id: `01JBX3K5Q7Z8M4N6P8R${String(index).padStart(7, '0')}`,
    name: `web-${String(index).padStart(4, '0')}`,
    displayName: '',
    hostname: `web-${String(index).padStart(4, '0')}`,
    tags: ['web'],
  }));
  return [...hard, ...plain];
}

/** worstMachine is the hardest machine view: every list at its limit. */
export const worstMachine: AgentDetails = {
  ...base,
  kernelVersion:
    '6.12.9-200.fc41.aarch64+debug #1 SMP PREEMPT_DYNAMIC Fri Nov  1 00:00:00 UTC 2026',
  cpuModel:
    'AMD EPYC 9965 192-Core Processor with Extended Instruction Sets and Very Long Marketing Name',
  cpuCores: 384,
  memoryTotalBytes: 2 * 1024 ** 5,
  capabilities: [
    ...['terminal', 'desktop', 'files', 'scripts', 'docker', 'storage', 'packages', 'power'].map(
      (name) => ({
        name,
        enabled: false,
        reason: 'Disabled by local policy',
      }),
    ),
    { name: 'an-unknown-capability-from-a-newer-agent', enabled: true, reason: '' },
  ],
  filesystems: Array.from({ length: 256 }, (_, index) => ({
    mount: `/mnt/backups/${'very-long-directory-name/'.repeat(19)}${String(index)}`,
    type: 'fuse.sshfs',
    usedBytes: index * 1024 ** 3,
    totalBytes: 512 * 1024 ** 4,
  })),
  temperatures: [],
};

export const worstTokens: EnrollmentToken[] = [
  {
    id: 1_000_000,
    note: 'Für die neuen Maschinen im Büro Frankfurt (Raum 4.12, Regal B) — bitte erst nach Freigabe!!',
    createdBy: longUsername,
    createdAt: fixtureNow - 3_600_000,
    expiresAt: fixtureNow + 7 * 86_400_000,
    maxUses: 1_000,
    useCount: 999,
    revoked: false,
    usable: true,
  },
  {
    id: 2,
    note: '',
    createdBy: 'x',
    createdAt: fixtureNow - 86_400_000,
    expiresAt: fixtureNow - 1,
    maxUses: 1,
    useCount: 0,
    revoked: false,
    usable: false,
  },
];
