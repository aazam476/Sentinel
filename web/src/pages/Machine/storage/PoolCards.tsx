// SPDX-License-Identifier: Apache-2.0
import type { ZFSARC, ZFSPool } from '../../../api/protocol.gen';
import { formatBytes, formatPercent } from '../../../formatting/formatting';
import { MetricMeter } from '../../Fleet/MetricMeter';
import { StateBadge } from './StateBadge';
import styles from './Storage.module.css';
import { describeScan, exactBytes, formatRatio, percentOf } from './storageText';

/** PoolCards shows one card per ZFS pool, then the ARC (§9.5). */
export function PoolCards({ pools, arc }: { pools: ZFSPool[]; arc?: ZFSARC }) {
  return (
    <section className={styles.section} aria-labelledby="pools-title">
      <h2 id="pools-title" className={styles.title}>
        Pools
      </h2>
      <div className={styles.cards}>
        {pools.map((pool) => (
          <PoolCard key={pool.name} pool={pool} />
        ))}
      </div>
      {arc !== undefined && <ARCPanel arc={arc} />}
    </section>
  );
}

function PoolCard({ pool }: { pool: ZFSPool }) {
  return (
    <article className={styles.card} aria-label={`Pool ${pool.name}`}>
      <header className={styles.cardHeader}>
        <span className={styles.name} title={pool.name}>
          {pool.name}
        </span>
        <StateBadge state={pool.health} />
      </header>
      <MetricMeter label="Used" value={percentOf(pool.allocatedBytes, pool.sizeBytes)} />
      <dl className={styles.facts}>
        <Fact label="Size" value={formatBytes(pool.sizeBytes)} title={exactBytes(pool.sizeBytes)} />
        <Fact
          label="Allocated"
          value={formatBytes(pool.allocatedBytes)}
          title={exactBytes(pool.allocatedBytes)}
        />
        <Fact label="Free" value={formatBytes(pool.freeBytes)} title={exactBytes(pool.freeBytes)} />
        {pool.fragmentationPercent !== undefined && (
          <Fact label="Fragmentation" value={formatPercent(pool.fragmentationPercent)} />
        )}
        <Fact label="Compression" value={formatRatio(pool.compressRatio)} />
        <Fact label="Dedup" value={formatRatio(pool.dedupRatio)} />
      </dl>
      <p className={styles.muted}>{describeScan(pool.scan)}</p>
      {pool.status !== undefined && pool.status !== '' && (
        <div className={styles.problem}>
          <p>{pool.status}</p>
          {pool.action !== undefined && pool.action !== '' && <p>{pool.action}</p>}
        </div>
      )}
      {pool.dataErrors !== '' && pool.dataErrors !== 'No known data errors' && (
        <p className={styles.problem}>{pool.dataErrors}</p>
      )}
    </article>
  );
}

export function Fact({ label, value, title }: { label: string; value: string; title?: string }) {
  return (
    <div className={styles.fact}>
      <dt>{label}</dt>
      <dd title={title}>{value}</dd>
    </div>
  );
}

function ARCPanel({ arc }: { arc: ZFSARC }) {
  const lookups = arc.hits + arc.misses;
  return (
    <section className={styles.card} aria-label="ARC">
      <span className={styles.name}>ARC (ZFS cache in memory)</span>
      <dl className={styles.facts}>
        <Fact label="Size" value={formatBytes(arc.sizeBytes)} title={exactBytes(arc.sizeBytes)} />
        <Fact
          label="Target max"
          value={formatBytes(arc.targetMaxBytes)}
          title={exactBytes(arc.targetMaxBytes)}
        />
        <Fact
          label="Hit ratio"
          value={lookups === 0 ? '–' : formatPercent(percentOf(arc.hits, lookups))}
        />
      </dl>
    </section>
  );
}
