// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';

import type { ZFSPool, ZFSVdev } from '../../../api/protocol.gen';
import { formatNumber } from '../../../formatting/formatting';
import { StateBadge } from './StateBadge';
import styles from './Storage.module.css';

const sectionLabels: Record<string, string> = {
  data: 'Data',
  log: 'Log',
  cache: 'Cache',
  spare: 'Spares',
  special: 'Special',
  dedup: 'Dedup',
};

/** VdevLayout shows each pool's vdev tree from zpool status (§9.5). */
export function VdevLayout({ pools }: { pools: ZFSPool[] }) {
  return (
    <section className={styles.section} aria-labelledby="vdevs-title">
      <h2 id="vdevs-title" className={styles.title}>
        Vdev layout
      </h2>
      {pools.map((pool) => (
        <div
          key={pool.name}
          className={styles.card}
          aria-label={`Vdevs of ${pool.name}`}
          role="group"
        >
          <span className={styles.name}>{pool.name}</span>
          {Object.entries(sectionLabels).map(([section, label]) => {
            const vdevs = pool.vdevs.filter((vdev) => vdev.section === section);
            return (
              vdevs.length > 0 && (
                <div key={section}>
                  <p className={styles.sectionLabel}>{label}</p>
                  <ul className={styles.vdevs}>
                    {vdevs.map((vdev) => (
                      <VdevItem key={vdev.name} vdev={vdev} />
                    ))}
                  </ul>
                </div>
              )
            );
          })}
        </div>
      ))}
    </section>
  );
}

function VdevItem({ vdev }: { vdev: ZFSVdev }) {
  const errors = vdev.readErrors + vdev.writeErrors + vdev.checksumErrors;
  return (
    <li>
      <span className={styles.vdev}>
        <span className={styles.path} title={vdev.name}>
          {vdev.name}
        </span>
        {vdev.state !== '' && <StateBadge state={vdev.state} />}
        {vdev.section !== 'spare' && (
          <span
            className={clsx(styles.counts, errors > 0 && styles.hasErrors)}
            title="Read, write and checksum errors"
          >
            {errors === 0
              ? 'No errors'
              : `${formatNumber(vdev.readErrors)} read · ${formatNumber(vdev.writeErrors)} write · ${formatNumber(vdev.checksumErrors)} checksum`}
          </span>
        )}
        {vdev.note !== undefined && vdev.note !== '' && (
          <span className={clsx(styles.muted, styles.path)}>{vdev.note}</span>
        )}
      </span>
      {vdev.children !== undefined && vdev.children.length > 0 && (
        <ul className={styles.vdevs}>
          {vdev.children.map((child) => (
            <VdevItem key={child.name} vdev={child} />
          ))}
        </ul>
      )}
    </li>
  );
}
