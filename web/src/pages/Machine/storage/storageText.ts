// SPDX-License-Identifier: Apache-2.0
import type { PhysicalDisk, ZFSScan } from '../../../api/protocol.gen';
import { formatDateTime, formatNumber } from '../../../formatting/formatting';

/** exactBytes is a size in bytes, for a tooltip beside a rounded size. */
export function exactBytes(bytes: number): string {
  return `${formatNumber(bytes)} bytes`;
}

/** percentOf is part of whole, 0–100; 0 when whole is 0. */
export function percentOf(part: number, whole: number): number {
  return whole === 0 ? 0 : (part / whole) * 100;
}

/** formatRatio shows a compression or dedup ratio as ZFS does: "1.07×". */
export function formatRatio(ratio: number): string {
  return `${new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(ratio)}×`;
}

/** describeScan says what a pool's last scrub or resilver did, in words. */
export function describeScan(scan: ZFSScan): string {
  const kind = scan.kind === 'resilver' ? 'Resilver' : 'Scrub';
  const when = scan.time !== undefined && scan.time !== 0 ? formatDateTime(scan.time) : '';
  switch (scan.state) {
    case 'none':
      return 'Never scrubbed';
    case 'finished':
      return `${kind} finished ${when} · ${scan.errors === 0 ? 'no errors' : `${formatNumber(scan.errors)} errors`}`;
    case 'in_progress':
      return `${kind} running since ${when}`;
    case 'paused':
      return `Scrub paused since ${when}`;
    case 'canceled':
      return `${kind} canceled ${when}`;
    default:
      return scan.text;
  }
}

/** healthClass picks the dot beside a pool or device state. */
export function healthTone(state: string): 'good' | 'warning' | 'bad' | 'neutral' {
  switch (state) {
    case 'ONLINE':
    case 'AVAIL':
      return 'good';
    case 'DEGRADED':
    case 'OFFLINE':
    case 'INUSE':
      return 'warning';
    case 'FAULTED':
    case 'UNAVAIL':
    case 'REMOVED':
    case 'SUSPENDED':
      return 'bad';
    default:
      return 'neutral';
  }
}

const kindLabels: Record<string, string> = {
  ssd: 'SSD',
  hdd: 'HDD',
  nvme: 'NVMe',
  unknown: 'Disk',
};

/** diskKind names a disk's type for people: "SSD", "HDD", "NVMe". */
export function diskKind(disk: PhysicalDisk): string {
  return kindLabels[disk.kind] ?? 'Disk';
}

/** membership says which pool a disk belongs to, as §9.5 words it. */
export function membership(disk: PhysicalDisk): string | undefined {
  if (disk.pool === undefined || disk.pool === '') {
    return undefined;
  }
  return disk.vdev === undefined || disk.vdev === ''
    ? `Member of ${disk.pool}`
    : `Member of ${disk.pool} (${disk.vdev})`;
}

/** isStorageAlert says whether an alert comes from storage health. */
export function isStorageAlert(kind: string): boolean {
  return kind.startsWith('zfs.') || kind.startsWith('smart.');
}
