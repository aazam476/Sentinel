// SPDX-License-Identifier: Apache-2.0
import type {
  Agent,
  AgentDetails,
  AuditEntry,
  CurrentMetrics,
  EnrollmentToken,
  LogEntries,
  PasskeySummary,
  ProcessList,
  ServiceList,
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

/** A machine's newest sample, as the fleet list and machine view see it. */
export const nasCurrent: CurrentMetrics = {
  time: fixtureNow - 5_000,
  uptimeSeconds: 3 * 86_400 + 4 * 3_600,
  values: {
    'cpu.percent': 23,
    'memory.percent': 61,
    'disk.percent': 92,
    'network.received': 1_258_291,
    'temperature.max': 51,
  },
};

export const agents: Agent[] = [
  {
    id: '01JBX3K5Q7Z8M4N6P8R0T2V4W6',
    name: 'Living room NAS',
    displayName: 'Living room NAS',
    hostname: 'nas-01',
    os: 'linux',
    platform: 'debian',
    platformVersion: '13',
    arch: 'amd64',
    agentVersion: 'v0.1.0',
    online: true,
    lastSeenAt: fixtureNow,
    enrolledAt: fixtureNow - 30 * 86_400_000,
    tags: ['homelab', 'storage'],
    firingAlerts: 0,
    current: nasCurrent,
  },
  {
    id: '01JBX3K5Q7Z8M4N6P8R0T2V4W7',
    name: 'web-01',
    displayName: '',
    hostname: 'web-01',
    os: 'linux',
    platform: 'ubuntu',
    platformVersion: '24.04',
    arch: 'arm64',
    agentVersion: 'v0.1.0',
    online: true,
    lastSeenAt: fixtureNow,
    enrolledAt: fixtureNow - 10 * 86_400_000,
    tags: ['office'],
    firingAlerts: 0,
    current: { time: fixtureNow, uptimeSeconds: 720, values: { 'cpu.percent': 4 } },
  },
  {
    id: '01JBX3K5Q7Z8M4N6P8R0T2V4W8',
    name: 'laptop',
    displayName: '',
    hostname: 'laptop',
    os: 'darwin',
    platform: 'darwin',
    platformVersion: '15.1',
    arch: 'arm64',
    agentVersion: 'v0.1.0',
    online: false,
    lastSeenAt: fixtureNow - 3 * 3_600_000,
    enrolledAt: fixtureNow - 86_400_000,
    tags: [],
    firingAlerts: 0,
    current: null,
  },
];

export const nasDetails: AgentDetails = {
  ...(agents[0] as Agent),
  kernelVersion: '6.12.9',
  cpuModel: 'Intel(R) N100',
  cpuCores: 4,
  memoryTotalBytes: 16 * 1024 ** 3,
  capabilities: [
    { name: 'terminal', enabled: true, reason: '' },
    { name: 'desktop', enabled: false, reason: 'Disabled by local policy' },
    { name: 'docker', enabled: true, reason: '' },
  ],
  filesystems: [
    { mount: '/', type: 'ext4', usedBytes: 20 * 1024 ** 3, totalBytes: 100 * 1024 ** 3 },
    { mount: '/srv', type: 'zfs', usedBytes: 9.2 * 1024 ** 4, totalBytes: 10 * 1024 ** 4 },
  ],
  temperatures: [{ sensor: 'coretemp', celsius: 51 }],
};

export const enrollmentTokens: EnrollmentToken[] = [
  {
    id: 3,
    note: 'office NAS',
    createdBy: 'admin',
    createdAt: fixtureNow - 3_600_000,
    expiresAt: fixtureNow + 23 * 3_600_000,
    maxUses: 1,
    useCount: 0,
    revoked: false,
    usable: true,
  },
  {
    id: 2,
    note: '',
    createdBy: 'admin',
    createdAt: fixtureNow - 86_400_000,
    expiresAt: fixtureNow + 86_400_000,
    maxUses: 2,
    useCount: 2,
    revoked: false,
    usable: false,
  },
  {
    id: 1,
    note: 'old laptop',
    createdBy: 'admin',
    createdAt: fixtureNow - 2 * 86_400_000,
    expiresAt: fixtureNow + 86_400_000,
    maxUses: 1,
    useCount: 0,
    revoked: true,
    usable: false,
  },
];

export const nasServices: ServiceList = {
  manager: 'systemd',
  services: [
    {
      name: 'docker.service',
      description: 'Docker Application Container Engine',
      state: 'failed',
      startup: 'automatic',
    },
    {
      name: 'nginx.service',
      description: 'A high performance web server',
      state: 'running',
      startup: 'automatic',
    },
    { name: 'nftables.service', description: 'nftables', state: 'stopped', startup: 'manual' },
    {
      name: 'dbus.service',
      description: 'D-Bus System Message Bus',
      state: 'running',
      startup: 'fixed',
    },
  ],
};

export const nasProcesses: ProcessList = {
  processes: [
    {
      pid: 1,
      name: 'systemd',
      user: 'root',
      cpuPercent: 0,
      memoryBytes: 12 * 1024 ** 2,
      command: '/sbin/init',
    },
    {
      pid: 900,
      name: 'postgres',
      user: 'postgres',
      cpuPercent: 12.5,
      memoryBytes: 1024 ** 3,
      command: 'postgres -D /var/lib/postgresql/16/main',
    },
    {
      pid: 4242,
      name: 'ffmpeg',
      user: 'media',
      cpuPercent: 187.3,
      memoryBytes: 512 * 1024 ** 2,
      command: 'ffmpeg -i movie.mkv -c:v libx265 out.mkv',
    },
  ],
};

export const nasLogs: LogEntries = {
  entries: [
    {
      time: fixtureNow - 1000,
      level: 'error',
      source: 'nginx',
      message: 'bind() to 0.0.0.0:80 failed (98: Address already in use)',
    },
    {
      time: fixtureNow - 5000,
      level: 'warning',
      source: 'kernel',
      message: 'EXT4-fs (sda1): warning: mounting fs with errors',
    },
    {
      time: fixtureNow - 9000,
      level: 'info',
      source: 'systemd',
      message: 'Started nginx.service.',
    },
  ],
  truncated: false,
};
