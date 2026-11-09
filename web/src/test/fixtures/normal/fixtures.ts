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
import type { FileListResultPayload, StorageOverviewPayload } from '../../../api/protocol.gen';

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
    maintenanceUntil: null,
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
    maintenanceUntil: null,
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
    maintenanceUntil: null,
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

/**
 * nasStorage is the storage of a NAS with a mirror pool and a degraded
 * raidz pool. Its numbers are the agent's golden zpool and zfs output
 * (internal/agent/storage/testdata), so the UI test checks the figures a
 * person would compare with zpool list and zfs list.
 */
export const nasStorage: StorageOverviewPayload = {
  zfs: { installed: true },
  pools: [
    {
      name: 'tank',
      health: 'ONLINE',
      sizeBytes: 3985729650688,
      allocatedBytes: 2817341710336,
      freeBytes: 1168387940352,
      fragmentationPercent: 12,
      dedupRatio: 1.07,
      compressRatio: 1.07,
      scan: {
        kind: 'scrub',
        state: 'finished',
        time: fixtureNow - 86_400_000,
        errors: 0,
        text: '',
      },
      dataErrors: 'No known data errors',
      vdevs: [
        {
          name: 'mirror-0',
          kind: 'mirror',
          section: 'data',
          state: 'ONLINE',
          readErrors: 0,
          writeErrors: 0,
          checksumErrors: 0,
          children: [
            {
              name: '/dev/disk/by-id/ata-WDC_WD20EFRX-68EUZN0_WD-WCC4M1234567',
              kind: 'device',
              section: 'data',
              state: 'ONLINE',
              readErrors: 0,
              writeErrors: 0,
              checksumErrors: 0,
            },
            {
              name: '/dev/disk/by-id/ata-WDC_WD20EFRX-68EUZN0_WD-WCC4M7654321',
              kind: 'device',
              section: 'data',
              state: 'ONLINE',
              readErrors: 0,
              writeErrors: 0,
              checksumErrors: 0,
            },
          ],
        },
        {
          name: '/dev/nvme0n1p5',
          kind: 'device',
          section: 'cache',
          state: 'ONLINE',
          readErrors: 0,
          writeErrors: 0,
          checksumErrors: 0,
        },
        {
          name: '/dev/sdf',
          kind: 'device',
          section: 'spare',
          state: 'AVAIL',
          readErrors: 0,
          writeErrors: 0,
          checksumErrors: 0,
        },
      ],
    },
    {
      name: 'backup',
      health: 'DEGRADED',
      sizeBytes: 2989297238016,
      allocatedBytes: 1203547291648,
      freeBytes: 1785749946368,
      fragmentationPercent: 3,
      dedupRatio: 1,
      compressRatio: 1,
      scan: {
        kind: 'scrub',
        state: 'finished',
        time: fixtureNow - 7 * 86_400_000,
        errors: 3,
        text: '',
      },
      status: 'One or more devices has been taken offline by the administrator.',
      action: 'Online the device using ‘zpool online’ or replace the device with ‘zpool replace’.',
      dataErrors: '3 data errors, use ‘-v’ for a list',
      vdevs: [
        {
          name: 'raidz1-0',
          kind: 'raidz1',
          section: 'data',
          state: 'DEGRADED',
          readErrors: 0,
          writeErrors: 0,
          checksumErrors: 0,
          children: [
            {
              name: '/dev/sdg1',
              kind: 'device',
              section: 'data',
              state: 'ONLINE',
              readErrors: 0,
              writeErrors: 0,
              checksumErrors: 2,
            },
            {
              name: '/dev/sdh1',
              kind: 'device',
              section: 'data',
              state: 'OFFLINE',
              readErrors: 0,
              writeErrors: 0,
              checksumErrors: 0,
            },
          ],
        },
      ],
    },
  ],
  datasets: [
    {
      name: 'tank',
      type: 'filesystem',
      usedBytes: 2817341710336,
      availableBytes: 1052138815488,
      referencedBytes: 196608,
      snapshotBytes: 0,
      childrenBytes: 2817341513728,
      quotaBytes: 0,
      reservationBytes: 0,
      volumeSizeBytes: 0,
      compressRatio: 1.07,
      mountpoint: '/tank',
    },
    {
      name: 'tank/media',
      type: 'filesystem',
      usedBytes: 2147483648000,
      availableBytes: 1052138815488,
      referencedBytes: 98304,
      snapshotBytes: 0,
      childrenBytes: 2147483549696,
      quotaBytes: 0,
      reservationBytes: 0,
      volumeSizeBytes: 0,
      compressRatio: 1.01,
      mountpoint: '/tank/media',
    },
    {
      name: 'tank/media/photos',
      type: 'filesystem',
      usedBytes: 644245094400,
      availableBytes: 1052138815488,
      referencedBytes: 601295421440,
      snapshotBytes: 42949672960,
      childrenBytes: 0,
      quotaBytes: 0,
      reservationBytes: 0,
      volumeSizeBytes: 0,
      compressRatio: 1.02,
      mountpoint: '/tank/media/photos',
    },
    {
      name: 'tank/home',
      type: 'filesystem',
      usedBytes: 91268055040,
      availableBytes: 16106127360,
      referencedBytes: 91268055040,
      snapshotBytes: 0,
      childrenBytes: 0,
      quotaBytes: 107374182400,
      reservationBytes: 0,
      volumeSizeBytes: 0,
      compressRatio: 1.43,
      mountpoint: '/home',
    },
    {
      name: 'tank/vm-disk',
      type: 'volume',
      usedBytes: 54760833024,
      availableBytes: 1106899648512,
      referencedBytes: 12884901888,
      snapshotBytes: 0,
      childrenBytes: 0,
      quotaBytes: 0,
      reservationBytes: 54760833024,
      volumeSizeBytes: 53687091200,
      compressRatio: 1.55,
      mountpoint: '',
    },
  ],
  arc: { sizeBytes: 8320421888, targetMaxBytes: 16847732736, hits: 918273645, misses: 10293847 },
  disks: [
    {
      name: 'sda',
      path: '/dev/sda',
      model: 'Samsung SSD 870 EVO 500GB',
      serial: 'S6PXNM0T401234A',
      sizeBytes: 500107862016,
      kind: 'ssd',
      smart: { passed: true, temperatureCelsius: 31, failing: [], warnings: [] },
    },
    {
      name: 'sdb',
      path: '/dev/sdb',
      model: 'WDC WD20EFRX-68EUZN0',
      serial: 'WD-WCC4M1234567',
      sizeBytes: 2000398934016,
      kind: 'hdd',
      smart: { passed: true, temperatureCelsius: 35, failing: [], warnings: [] },
      pool: 'tank',
      vdev: 'mirror-0',
    },
    {
      name: 'sdd',
      path: '/dev/sdd',
      model: 'ST2000DM008-2FR102',
      serial: 'ZFL1ABCD',
      sizeBytes: 2000398934016,
      kind: 'hdd',
      smart: {
        passed: false,
        temperatureCelsius: 41,
        failing: ['Reallocated_Sector_Ct'],
        warnings: ['Spin_Retry_Count'],
      },
      pool: 'backup',
      vdev: 'raidz1-0',
    },
    {
      name: 'nvme0n1',
      path: '/dev/nvme0n1',
      model: 'Samsung SSD 980 PRO 1TB',
      serial: 'S5GXNX0T912345K',
      sizeBytes: 1000204886016,
      kind: 'nvme',
      pool: 'tank',
      vdev: 'cache',
    },
  ],
  smartAvailable: true,
  filesystems: [
    {
      mount: '/',
      device: '/dev/sda2',
      type: 'ext4',
      usedBytes: 120 * 1024 ** 3,
      totalBytes: 465 * 1024 ** 3,
      system: false,
    },
    {
      mount: '/boot/efi',
      device: '/dev/sda1',
      type: 'vfat',
      usedBytes: 6 * 1024 ** 2,
      totalBytes: 512 * 1024 ** 2,
      system: false,
    },
    {
      mount: '/snap/core22/1380',
      device: '/dev/loop0',
      type: 'squashfs',
      usedBytes: 77 * 1024 ** 2,
      totalBytes: 77 * 1024 ** 2,
      system: true,
    },
  ],
};

/** nasFolder is /srv/media on the NAS: folders first, a link, and files. */
export const nasFolder: FileListResultPayload = {
  path: '/srv/media',
  parent: '/srv',
  truncated: false,
  entries: [
    {
      name: 'Photos',
      path: '/srv/media/Photos',
      type: 'dir',
      sizeBytes: 0,
      mode: 'drwxr-xr-x',
      modified: fixtureNow - 86_400_000,
    },
    {
      name: 'current',
      path: '/srv/media/current',
      type: 'symlink',
      sizeBytes: 0,
      mode: 'Lrwxrwxrwx',
      modified: fixtureNow,
      target: '/srv/media/Photos',
    },
    {
      name: 'backup.tar.gz',
      path: '/srv/media/backup.tar.gz',
      type: 'file',
      sizeBytes: 3 * 1024 ** 3,
      mode: '-rw-r--r--',
      modified: fixtureNow - 3_600_000,
    },
    {
      name: 'notes.txt',
      path: '/srv/media/notes.txt',
      type: 'file',
      sizeBytes: 812,
      mode: '-rw-------',
      modified: fixtureNow - 60_000,
    },
  ],
};
