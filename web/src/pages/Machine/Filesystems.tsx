// SPDX-License-Identifier: Apache-2.0
import type { Filesystem } from '../../api/types.gen';
import { formatBytes, formatPercent } from '../../formatting/formatting';
import styles from './MachinePage.module.css';

/** Filesystems lists mounted filesystems from the newest sample, fullest first. */
export function Filesystems({
  filesystems,
  isOnline,
}: {
  filesystems: Filesystem[];
  isOnline: boolean;
}) {
  const sorted = [...filesystems].sort((a, b) => usage(b) - usage(a));
  return (
    <section className={styles.panel} aria-labelledby="filesystems-title">
      <h2 id="filesystems-title" className={styles.panelTitle}>
        Filesystems
      </h2>
      {sorted.length === 0 ? (
        <p className={styles.muted}>
          {isOnline ? 'Waiting for the first reading.' : 'Shown while the machine is online.'}
        </p>
      ) : (
        <ul className={styles.filesystems}>
          {sorted.map((filesystem) => (
            <li key={filesystem.mount} className={styles.filesystem}>
              <span className={styles.mount} title={filesystem.mount}>
                {filesystem.mount}
              </span>
              <span className={styles.muted}>{filesystem.type}</span>
              <span className={styles.usage}>
                {formatBytes(filesystem.usedBytes)} of {formatBytes(filesystem.totalBytes)} ·{' '}
                {formatPercent(usage(filesystem))}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function usage(filesystem: Filesystem): number {
  return filesystem.totalBytes === 0 ? 0 : (filesystem.usedBytes / filesystem.totalBytes) * 100;
}
