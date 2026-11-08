// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';

import type { StorageFilesystem } from '../../../api/protocol.gen';
import { formatBytes, formatNumber } from '../../../formatting/formatting';
import { MetricMeter } from '../../Fleet/MetricMeter';
import styles from './Storage.module.css';
import { exactBytes, percentOf } from './storageText';

/**
 * OtherFilesystems lists mounts that aren't ZFS datasets, fullest first.
 * System mounts (tmpfs, snaps, container layers) are hidden until asked for.
 */
export function OtherFilesystems({ filesystems }: { filesystems: StorageFilesystem[] }) {
  const [showSystem, setShowSystem] = useState(false);
  const systemCount = filesystems.filter((filesystem) => filesystem.system).length;
  const shown = filesystems
    .filter((filesystem) => showSystem || !filesystem.system)
    .sort((a, b) => percentOf(b.usedBytes, b.totalBytes) - percentOf(a.usedBytes, a.totalBytes));
  return (
    <section className={styles.section} aria-labelledby="filesystems-title">
      <div className={styles.sectionHeader}>
        <h2 id="filesystems-title" className={styles.title}>
          Other filesystems
        </h2>
        {systemCount > 0 && (
          <label className={styles.toggle}>
            <input
              type="checkbox"
              checked={showSystem}
              onChange={(event) => {
                setShowSystem(event.target.checked);
              }}
            />
            Show system mounts ({formatNumber(systemCount)})
          </label>
        )}
      </div>
      {shown.length === 0 ? (
        <p className={styles.muted}>No other filesystems.</p>
      ) : (
        <ul className={styles.list}>
          {shown.map((filesystem) => (
            <li
              key={filesystem.mount}
              className={styles.row}
              aria-label={`Filesystem ${filesystem.mount}`}
            >
              <span className={styles.rowTitle}>
                <span className={styles.path} title={filesystem.mount}>
                  {filesystem.mount}
                </span>
                <span className={styles.muted}>{filesystem.type}</span>
              </span>
              <span className={styles.meta}>
                <span className={styles.path} title={filesystem.device}>
                  {filesystem.device}
                </span>
                <span
                  title={`${exactBytes(filesystem.usedBytes)} of ${exactBytes(filesystem.totalBytes)}`}
                >
                  {formatBytes(filesystem.usedBytes)} of {formatBytes(filesystem.totalBytes)}
                </span>
              </span>
              <span className={styles.quota}>
                <MetricMeter
                  label="Used"
                  value={percentOf(filesystem.usedBytes, filesystem.totalBytes)}
                />
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
