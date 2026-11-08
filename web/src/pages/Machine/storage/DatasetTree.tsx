// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { useState } from 'react';

import type { ZFSDataset } from '../../../api/protocol.gen';
import { formatBytes, formatNumber } from '../../../formatting/formatting';
import { MetricMeter } from '../../Fleet/MetricMeter';
import machineStyles from '../MachinePage.module.css';
import systemStyles from '../SystemTab.module.css';
import styles from './Storage.module.css';
import { exactBytes, formatRatio, percentOf } from './storageText';

type Order = 'tree' | 'used';

/**
 * shownAtFirst is how many datasets show before "Show all". Docker's ZFS
 * storage driver makes a dataset per image layer, which can be thousands.
 */
const shownAtFirst = 200;

const sharedSpace =
  'Datasets share their pool’s free space, so datasets in the same pool usually show the same available space.';

/**
 * DatasetTree lists every dataset and zvol, as a tree mirroring their
 * hierarchy or largest first, to find what uses the space (§9.5).
 */
export function DatasetTree({ datasets }: { datasets: ZFSDataset[] }) {
  const [order, setOrder] = useState<Order>('tree');
  const [showAll, setShowAll] = useState(false);
  const sorted =
    order === 'used'
      ? [...datasets].sort((a, b) => b.usedBytes - a.usedBytes)
      : [...datasets].sort((a, b) => a.name.localeCompare(b.name));
  const shown = showAll ? sorted : sorted.slice(0, shownAtFirst);
  return (
    <section className={styles.section} aria-labelledby="datasets-title">
      <div className={styles.sectionHeader}>
        <h2 id="datasets-title" className={styles.title}>
          Datasets
        </h2>
        <div className={machineStyles.segmented} role="group" aria-label="Order datasets">
          {(
            [
              ['tree', 'Tree'],
              ['used', 'Largest first'],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              type="button"
              aria-pressed={order === value}
              className={clsx(
                machineStyles.segment,
                order === value && machineStyles.segmentActive,
              )}
              onClick={() => {
                setOrder(value);
              }}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      <p className={styles.muted}>{sharedSpace}</p>
      <ul className={styles.list}>
        {shown.map((dataset) => (
          <DatasetRow key={dataset.name} dataset={dataset} isTree={order === 'tree'} />
        ))}
      </ul>
      {shown.length < sorted.length && (
        <button
          type="button"
          className={systemStyles.linkButton}
          onClick={() => {
            setShowAll(true);
          }}
        >
          Show all {formatNumber(sorted.length)} datasets
        </button>
      )}
    </section>
  );
}

function DatasetRow({ dataset, isTree }: { dataset: ZFSDataset; isTree: boolean }) {
  const parts = dataset.name.split('/');
  const depth = parts.length - 1;
  const isVolume = dataset.type === 'volume';
  return (
    <li
      className={styles.row}
      aria-label={`${isVolume ? 'Volume' : 'Dataset'} ${dataset.name}`}
      data-depth={isTree ? Math.min(depth, 8) : 0}
    >
      <span className={styles.rowTitle}>
        <span className={styles.icon} aria-hidden="true">
          {isVolume ? '▣' : '▤'}
        </span>
        <span className={styles.name} title={dataset.name}>
          {isTree ? parts[parts.length - 1] : dataset.name}
        </span>
        {isVolume && (
          <span className={styles.muted}>volume · {formatBytes(dataset.volumeSizeBytes)}</span>
        )}
      </span>
      <span className={styles.meta}>
        <Size label="Used" bytes={dataset.usedBytes} />
        <span title={sharedSpace}>
          Available <Bytes bytes={dataset.availableBytes} />
        </span>
        <Size label="Referenced" bytes={dataset.referencedBytes} />
        {dataset.snapshotBytes > 0 && <Size label="Snapshots" bytes={dataset.snapshotBytes} />}
        {dataset.childrenBytes > 0 && <Size label="Children" bytes={dataset.childrenBytes} />}
        {dataset.reservationBytes > 0 && <Size label="Reserved" bytes={dataset.reservationBytes} />}
        <span>Compression {formatRatio(dataset.compressRatio)}</span>
        {dataset.mountpoint !== '' && (
          <span className={styles.path} title={dataset.mountpoint}>
            {dataset.mountpoint}
          </span>
        )}
      </span>
      {dataset.quotaBytes > 0 && (
        <span className={styles.quota}>
          <MetricMeter
            label={`Quota ${formatBytes(dataset.quotaBytes)}`}
            value={percentOf(dataset.usedBytes, dataset.quotaBytes)}
          />
        </span>
      )}
    </li>
  );
}

function Bytes({ bytes }: { bytes: number }) {
  return <span title={exactBytes(bytes)}>{formatBytes(bytes)}</span>;
}

function Size({ label, bytes }: { label: string; bytes: number }) {
  return (
    <span>
      {label} <Bytes bytes={bytes} />
    </span>
  );
}
