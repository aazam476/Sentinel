// SPDX-License-Identifier: Apache-2.0
import type {
  Agent,
  AgentDetails,
  AuditEntry,
  EnrollmentToken,
  LogEntries,
  PasskeySummary,
  ProcessList,
  ServiceList,
  SessionResponse,
  SessionSummary,
} from '../../../api/types.gen';
import type {
  ContainerListPayload,
  FileEntry,
  FileListResultPayload,
  StackListPayload,
  StorageOverviewPayload,
} from '../../../api/protocol.gen';
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
  firingAlerts: 1284,
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
      firingAlerts: 0,
    },
    {
      ...base,
      id: '01JBX3K5Q7Z8M4N6P8R0T2V4W2',
      name: 'x',
      displayName: '',
      hostname: 'x',
      tags: [],
      firingAlerts: 0,
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
    firingAlerts: 0,
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

/**
 * worstServices: systemd's escaped instance names, a description in Arabic,
 * one with no description, every state and startup mode, and 1,200 more so
 * the list must virtualize.
 */
export function worstServices(): ServiceList {
  const hard: ServiceList['services'] = [
    {
      name: 'systemd-fsck@dev-disk-by\\x2duuid-0b5c8f3e\\x2d1d2a\\x2d4c6b\\x2d9e1f\\x2d7a3b2c1d0e9f.service',
      description:
        'File System Check on /dev/disk/by-uuid/0b5c8f3e-1d2a-4c6b-9e1f-7a3b2c1d0e9f and a description long enough to need cutting',
      state: 'failed',
      startup: 'fixed',
    },
    {
      name: 'خدمة-النسخ-الاحتياطي.service',
      description: 'النسخ الاحتياطي الليلي للخادم',
      state: 'running',
      startup: 'automatic',
    },
    { name: 'a.service', description: '', state: 'unknown', startup: 'manual' },
    {
      name: 'MSSQL$SQLEXPRESS',
      description: 'SQL Server (SQLEXPRESS) 🚀',
      state: 'starting',
      startup: 'disabled',
    },
    {
      name: 'com.example.helper-with-a-very-long-reverse-dns-label-from-a-vendor-installer',
      description: 'Helper',
      state: 'stopping',
      startup: 'automatic',
    },
  ];
  const plain = Array.from({ length: 1200 }, (_, index) => ({
    name: `worker@${String(index).padStart(4, '0')}.service`,
    description: `Queue worker ${String(index)}`,
    state: index % 7 === 0 ? ('failed' as const) : ('running' as const),
    startup: 'automatic' as const,
  }));
  return { manager: 'systemd', services: [...hard, ...plain] };
}

/** worstProcesses: a 4 KB command line, 32 cores busy, 1.5 TB of memory, no name, 1,200 rows. */
export function worstProcesses(): ProcessList {
  const hard: ProcessList['processes'] = [
    {
      pid: 2_147_483_647,
      name: 'java',
      user: longUsername,
      cpuPercent: 3187.46,
      memoryBytes: 1.5 * 1024 ** 4,
      command: `/usr/lib/jvm/java-21/bin/java -Xmx1400g -cp ${'/opt/app/lib/very-long-library-name-1.2.3.jar:'.repeat(80)} com.example.Main`,
    },
    { pid: 2, name: '', user: '', cpuPercent: 0, memoryBytes: 0, command: '' },
    {
      pid: 3,
      name: 'محرك-البحث',
      user: 'مستخدم',
      cpuPercent: 0.04,
      memoryBytes: 1,
      command: 'محرك-البحث --لغة=العربية',
    },
    {
      pid: 4,
      name: 'node 🚀',
      user: 'www-data',
      cpuPercent: 99.99,
      memoryBytes: 512,
      command: 'node server.js',
    },
  ];
  const plain = Array.from({ length: 1200 }, (_, index) => ({
    pid: 10_000 + index,
    name: `php-fpm: pool www`,
    user: 'www-data',
    cpuPercent: (index % 100) / 10,
    memoryBytes: (index + 1) * 1024 ** 2,
    command: 'php-fpm: pool www',
  }));
  return { processes: [...hard, ...plain] };
}

/** worstLogs: an unbreakable base64 blob, a multi-line stack trace, RTL, no source, 1,000 entries, cut off. */
export function worstLogs(): LogEntries {
  const hard: LogEntries['entries'] = [
    {
      time: fixtureNow - 1000,
      level: 'error',
      source: 'kernel-module-with-an-unusually-long-identifier-that-goes-on',
      message: `certificate rejected: ${'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA'.repeat(20)}`,
    },
    {
      time: fixtureNow - 2000,
      level: 'warning',
      source: 'app',
      message:
        'Traceback (most recent call last):\n  File "/srv/app.py", line 42, in <module>\n    main()\nValueError: 💥',
    },
    {
      time: fixtureNow - 3000,
      level: 'notice',
      source: '',
      message: 'فشل الاتصال بقاعدة البيانات',
    },
    { time: fixtureNow - 4000, level: 'debug', source: 'x', message: '' },
  ];
  const plain = Array.from({ length: 996 }, (_, index) => ({
    time: fixtureNow - 5000 - index * 1000,
    level: 'info' as const,
    source: 'cron',
    message: `(root) CMD (run-parts /etc/cron.hourly) #${String(index)}`,
  }));
  return { entries: [...hard, ...plain], truncated: true };
}

/** worstContainers: compose's long generated names, many ports, RTL, 1,200 rows. */
export function worstContainers(): ContainerListPayload {
  const hard: ContainerListPayload['containers'] = [
    {
      id: 'a'.repeat(64),
      name: 'production-europe-west-ecommerce-platform-checkout-service-worker-replica-12',
      image:
        'registry.internal.northwind-industries-holdings.example.com:5000/platform/checkout-service@sha256:' +
        'f'.repeat(64),
      state: 'restarting',
      status: 'Restarting (137) 3 seconds ago',
      created: fixtureNow,
      ports: Array.from({ length: 12 }, (_, i) => ({
        ip: '0.0.0.0',
        privatePort: 8000 + i,
        publicPort: 18000 + i,
        protocol: 'tcp',
      })),
      stack: 'production-europe-west-ecommerce-platform',
      service: 'checkout-service-worker',
      cpuPercent: 3199.97,
      memoryBytes: 1.9 * 1024 ** 4,
      memoryLimitBytes: 2 * 1024 ** 4,
    },
    {
      id: 'b',
      name: 'خدمة-الويب',
      image: 'nginx',
      state: 'running',
      status: 'Up',
      created: 0,
      ports: [],
      cpuPercent: 0,
      memoryBytes: 0,
      memoryLimitBytes: 0,
    },
    {
      id: 'c',
      name: 'x',
      image: 'x',
      state: 'dead',
      status: '',
      created: 0,
      ports: [],
      cpuPercent: 0,
      memoryBytes: 0,
      memoryLimitBytes: 0,
    },
  ];
  const plain = Array.from({ length: 1200 }, (_, i) => ({
    id: `p${String(i)}`,
    name: `worker-${String(i).padStart(4, '0')}`,
    image: 'busybox:1.36',
    state: 'running',
    status: 'Up 2 days',
    created: 0,
    ports: [],
    cpuPercent: (i % 50) / 10,
    memoryBytes: i * 1024 ** 2,
    memoryLimitBytes: 0,
  }));
  return {
    engine: { name: 'Podman', version: '5.4.2', socket: 'unix:///run/podman/podman.sock' },
    containers: [...hard, ...plain],
  };
}

/** worstStacks: a long name, an unreadable external stack with many files, none running. */
export const worstStacks: StackListPayload = {
  dir: '/srv/sentinel/stacks-with-a-long-folder-name-for-every-environment',
  stacks: [
    {
      name: 'production-europe-west-ecommerce-platform-checkout-stack-0001',
      managed: true,
      containers: { running: 120, exited: 3, restarting: 1 },
      configFiles: [
        '/srv/sentinel/stacks-with-a-long-folder-name-for-every-environment/production-europe-west-ecommerce-platform-checkout-stack-0001/compose.yaml',
      ],
      editable: true,
    },
    {
      name: 'legacy',
      managed: false,
      containers: {},
      configFiles: [
        '/home/admin/projects/legacy/docker-compose.yml',
        '/home/admin/projects/legacy/docker-compose.override.yml',
      ],
      editable: false,
    },
  ],
};

/** A disk's by-id path as udev names it, for SAS disks behind an expander. */
const byId = (index: number) =>
  `/dev/disk/by-id/scsi-SATA_HGST_HUH721212ALE604_8HJ${String(index).padStart(5, '0')}ABCDEFGH-part1`;

const longPool = 'northwind-industries-holdings-archive-pool-for-long-term-retention-2026';

/**
 * worstStorage: a pool name at 72 characters, a FAULTED pool with zpool's
 * longest status text, petabyte sizes, a 24-wide dRAID of by-id paths with
 * million-count errors, 1,503 datasets (Docker's ZFS driver makes one per
 * layer) nested 12 deep with spaces, 60 disks failing SMART, a mountpoint
 * of 200 characters and 300 system mounts.
 */
export function worstStorage(): StorageOverviewPayload {
  const members = Array.from({ length: 24 }, (_, index) => ({
    name: byId(index),
    kind: 'device',
    section: 'data',
    state: index === 3 ? 'UNAVAIL' : index === 7 ? 'FAULTED' : 'ONLINE',
    readErrors: index === 7 ? 1_284_903 : 0,
    writeErrors: index === 7 ? 88_201 : 0,
    checksumErrors: index === 7 ? 4_294_967_295 : 0,
    note: index === 3 ? `was ${byId(99)}` : undefined,
  }));
  const deep = Array.from({ length: 12 }, (_, depth) =>
    [
      longPool,
      ...Array.from(
        { length: depth + 1 },
        (_, level) => `level ${String(level)} with spaces and a long name`,
      ),
    ].join('/'),
  );
  const layers = Array.from(
    { length: 1_490 },
    (_, index) => `${longPool}/docker/${index.toString(16).padStart(64, 'f')}`,
  );
  return {
    zfs: { installed: true },
    pools: [
      {
        name: longPool,
        health: 'FAULTED',
        sizeBytes: 5 * 1024 ** 5,
        allocatedBytes: 5 * 1024 ** 5 - 1,
        freeBytes: 1,
        dedupRatio: 12.47,
        compressRatio: 1,
        scan: {
          kind: 'resilver',
          state: 'in_progress',
          time: fixtureNow - 400 * 86_400_000,
          errors: 0,
          text: '',
        },
        status:
          'One or more devices are faulted in response to persistent errors. There are insufficient replicas for the pool to continue functioning. The pool has been suspended because I/O to it failed; applications that use it may hang until it is resumed.',
        action:
          'Destroy and re-create the pool from a backup source. Manually marking the device repaired using ‘zpool clear’ may allow some data to be recovered. See https://openzfs.github.io/openzfs-docs/msg/ZFS-8000-JQ for details on the error.',
        dataErrors:
          'Permanent errors have been detected in the following files: <metadata>:<0x0> <metadata>:<0x1b>',
        vdevs: [
          {
            name: 'draid2:16d:24c:2s-0',
            kind: 'draid',
            section: 'data',
            state: 'FAULTED',
            readErrors: 0,
            writeErrors: 0,
            checksumErrors: 0,
            children: members,
          },
          {
            name: byId(500),
            kind: 'device',
            section: 'log',
            state: 'REMOVED',
            readErrors: 0,
            writeErrors: 0,
            checksumErrors: 0,
          },
        ],
      },
    ],
    datasets: [
      {
        name: longPool,
        type: 'filesystem',
        usedBytes: 5 * 1024 ** 5,
        availableBytes: 0,
        referencedBytes: 0,
        snapshotBytes: 0,
        childrenBytes: 5 * 1024 ** 5,
        quotaBytes: 0,
        reservationBytes: 0,
        volumeSizeBytes: 0,
        compressRatio: 1,
        mountpoint: `/mnt/${'archive-of-everything-that-ever-mattered/'.repeat(4)}`.slice(0, 200),
      },
      ...deep.map((name, index) => ({
        name,
        type: 'filesystem',
        usedBytes: 1024 ** 4 * (12 - index),
        availableBytes: 0,
        referencedBytes: 1024 ** 3,
        snapshotBytes: 1024 ** 4,
        childrenBytes: 1024 ** 4,
        quotaBytes: index === 11 ? 1024 ** 4 : 0,
        reservationBytes: index === 5 ? 1024 ** 3 : 0,
        volumeSizeBytes: 0,
        compressRatio: 3.99,
        mountpoint: index % 2 === 0 ? '' : `/${name}`,
      })),
      ...layers.map((name, index) => ({
        name,
        type: index % 100 === 0 ? 'volume' : 'filesystem',
        usedBytes: index * 4096,
        availableBytes: 0,
        referencedBytes: 4096,
        snapshotBytes: 0,
        childrenBytes: 0,
        quotaBytes: 0,
        reservationBytes: 0,
        volumeSizeBytes: index % 100 === 0 ? 1024 ** 4 : 0,
        compressRatio: 1,
        mountpoint: '',
      })),
    ],
    arc: { sizeBytes: 0, targetMaxBytes: 0, hits: 0, misses: 0 },
    disks: Array.from({ length: 60 }, (_, index) => ({
      name: `sd${String.fromCharCode(97 + (index % 26))}${index >= 26 ? String.fromCharCode(97 + Math.floor(index / 26) - 1) : ''}`,
      path: byId(index).replace('-part1', ''),
      model:
        'HGST Ultrastar DC HC520 HUH721212ALE604 Enterprise Helium Platform 12TB SATA 6Gb/s 7200RPM',
      serial: `8HJ${String(index).padStart(5, '0')}ABCDEFGH0123456789`,
      sizeBytes: 12 * 1000 ** 4,
      kind: index % 3 === 0 ? 'unknown' : 'hdd',
      smart:
        index % 4 === 0
          ? undefined
          : {
              passed: index % 5 !== 0,
              temperatureCelsius: 71.5,
              failing:
                index % 5 === 0
                  ? [
                      'Reallocated_Sector_Ct',
                      'Current_Pending_Sector',
                      'Offline_Uncorrectable',
                      'UDMA_CRC_Error_Count',
                    ]
                  : [],
              warnings: ['Spin_Retry_Count', 'NVMe critical warning 0x1f'],
            },
      pool: index < 24 ? longPool : undefined,
      vdev: index < 24 ? 'draid2:16d:24c:2s-0' : undefined,
    })),
    smartAvailable: true,
    filesystems: [
      {
        mount: `/mnt/remote/${'department-of-very-long-folder-names/'.repeat(5)}`.slice(0, 200),
        device: '//fileserver.northwind-industries-holdings.example.com/Shared Documents (Archive)',
        type: 'cifs',
        usedBytes: 1023 * 1024 ** 4,
        totalBytes: 1024 ** 5,
        system: false,
      },
      {
        mount: '/',
        device: '/dev/mapper/ubuntu--vg-ubuntu--lv',
        type: 'ext4',
        usedBytes: 0,
        totalBytes: 1,
        system: false,
      },
      ...Array.from({ length: 300 }, (_, index) => ({
        mount: `/var/lib/docker/overlay2/${index.toString(16).padStart(64, 'a')}/merged`,
        device: 'overlay',
        type: 'overlay',
        usedBytes: 1,
        totalBytes: 2,
        system: true,
      })),
    ],
  };
}

const worstFolderPath = `/srv/${'department-of-very-long-folder-names/'.repeat(5)}archive`;

/**
 * worstFolder: a folder at the 5,000-entry limit, names of 255 characters
 * with no spaces, right-to-left and emoji names, a link to a long target,
 * a 999 TB file and a socket.
 */
export function worstFolder(): FileListResultPayload {
  const special: FileEntry[] = [
    {
      name: 'x'.repeat(255),
      path: `${worstFolderPath}/${'x'.repeat(255)}`,
      type: 'dir',
      sizeBytes: 0,
      mode: 'drwxrwsrwt',
      modified: 0,
    },
    {
      name: `${'a'.repeat(200)}.tar.gz`,
      path: `${worstFolderPath}/${'a'.repeat(200)}.tar.gz`,
      type: 'file',
      sizeBytes: 999 * 1024 ** 4,
      mode: '-rwsr-sr-x',
      modified: fixtureNow,
    },
    {
      name: 'تقرير-المبيعات-٢٠٢٦.pdf',
      path: `${worstFolderPath}/تقرير-المبيعات-٢٠٢٦.pdf`,
      type: 'file',
      sizeBytes: 1,
      mode: '-rw-------',
      modified: fixtureNow,
    },
    {
      name: '📸 Holiday photos 🏖️ (final) (2).zip',
      path: `${worstFolderPath}/📸 Holiday photos 🏖️ (final) (2).zip`,
      type: 'file',
      sizeBytes: 0,
      mode: '-rw-r--r--',
      modified: fixtureNow,
    },
    {
      name: 'latest',
      path: `${worstFolderPath}/latest`,
      type: 'symlink',
      sizeBytes: 0,
      mode: 'Lrwxrwxrwx',
      modified: fixtureNow,
      target: `/mnt/${'snapshots/'.repeat(20)}2026-11-08T03:00:00Z`,
    },
    {
      name: 'docker.sock',
      path: `${worstFolderPath}/docker.sock`,
      type: 'other',
      sizeBytes: 0,
      mode: 'Srw-rw----',
      modified: fixtureNow,
    },
  ];
  const many = Array.from({ length: 5_000 - special.length }, (_, index): FileEntry => ({
    name: `log-${String(index).padStart(5, '0')}.jsonl`,
    path: `${worstFolderPath}/log-${String(index).padStart(5, '0')}.jsonl`,
    type: 'file',
    sizeBytes: index * 1_024,
    mode: '-rw-r-----',
    modified: fixtureNow - index * 60_000,
  }));
  return {
    path: worstFolderPath,
    parent: worstFolderPath.slice(0, worstFolderPath.lastIndexOf('/')),
    truncated: true,
    entries: [...special, ...many],
  };
}

export const worstFolderLink = `/machines/${worstMachine.id}/files?path=${encodeURIComponent(worstFolderPath)}`;
