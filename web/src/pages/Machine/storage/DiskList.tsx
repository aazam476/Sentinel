// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';

import type { PhysicalDisk } from '../../../api/protocol.gen';
import { formatBytes, formatNumber } from '../../../formatting/formatting';
import systemStyles from '../SystemTab.module.css';
import styles from './Storage.module.css';
import { diskKind, exactBytes, membership } from './storageText';

/**
 * DiskList shows every physical disk with its SMART health. A disk in a
 * ZFS pool says which, instead of a usage figure that would mislead (§9.5).
 */
export function DiskList({
  disks,
  smartAvailable,
}: {
  disks: PhysicalDisk[];
  smartAvailable: boolean;
}) {
  const anySMART = disks.some((disk) => disk.smart !== undefined);
  return (
    <section className={styles.section} aria-labelledby="disks-title">
      <h2 id="disks-title" className={styles.title}>
        Physical disks
      </h2>
      {!smartAvailable && !anySMART && (
        <p className={styles.muted}>
          Install smartmontools on the machine to see SMART health and temperatures.
        </p>
      )}
      {disks.length === 0 ? (
        <p className={styles.muted}>No physical disks found.</p>
      ) : (
        <ul className={styles.list}>
          {disks.map((disk) => (
            <DiskRow key={disk.path} disk={disk} />
          ))}
        </ul>
      )}
    </section>
  );
}

function DiskRow({ disk }: { disk: PhysicalDisk }) {
  const member = membership(disk);
  return (
    <li className={styles.row} aria-label={`Disk ${disk.name}`}>
      <span className={styles.rowTitle}>
        <span className={styles.name} title={disk.path}>
          {disk.name}
        </span>
        <span className={styles.model} title={disk.model}>
          {disk.model}
        </span>
        <SMARTBadge disk={disk} />
      </span>
      <span className={styles.meta}>
        <span>{diskKind(disk)}</span>
        {disk.sizeBytes > 0 && (
          <span title={exactBytes(disk.sizeBytes)}>{formatBytes(disk.sizeBytes)}</span>
        )}
        {disk.serial !== '' && (
          <span className={styles.path} title={disk.serial}>
            {disk.serial}
          </span>
        )}
        {disk.smart?.temperatureCelsius !== undefined && (
          <span>{formatNumber(disk.smart.temperatureCelsius)} °C</span>
        )}
        <span className={clsx(member !== undefined && styles.member)}>
          {member ?? 'Not in a pool'}
        </span>
      </span>
      {disk.smart !== undefined && disk.smart.failing.length + disk.smart.warnings.length > 0 && (
        <span className={styles.problem}>
          {[...disk.smart.failing.map((name) => `${name} failing`), ...disk.smart.warnings].join(
            ' · ',
          )}
        </span>
      )}
    </li>
  );
}

function SMARTBadge({ disk }: { disk: PhysicalDisk }) {
  if (disk.smart === undefined) {
    return <span className={systemStyles.badge}>SMART unknown</span>;
  }
  const failing = !disk.smart.passed || disk.smart.failing.length > 0;
  const warning = !failing && disk.smart.warnings.length > 0;
  return (
    <span
      className={clsx(
        systemStyles.badge,
        failing ? systemStyles.failed : warning ? systemStyles.warning : systemStyles.running,
      )}
    >
      {failing ? 'SMART failing' : warning ? 'SMART warning' : 'SMART OK'}
    </span>
  );
}
