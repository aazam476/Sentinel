// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';

import { formatPercent } from '../../formatting/formatting';
import styles from './MetricMeter.module.css';

interface MetricMeterProps {
  label: string;
  /** value is 0–100, or undefined when the machine doesn't report it. */
  value: number | undefined;
}

/**
 * MetricMeter is a small labelled bar for a percentage. It doesn't animate:
 * it updates every 10 seconds on a screen people keep open.
 */
export function MetricMeter({ label, value }: MetricMeterProps) {
  if (value === undefined) {
    return (
      <div className={styles.meter}>
        <span className={styles.label}>{label}</span>
        <span className={styles.value} aria-label={`${label}: not reported`}>
          –
        </span>
      </div>
    );
  }
  const level = value >= 90 ? styles.critical : value >= 75 ? styles.high : undefined;
  return (
    <div className={styles.meter}>
      <span className={styles.label}>{label}</span>
      <span className={clsx(styles.value, level)}>{formatPercent(value)}</span>
      <span className={styles.track} aria-hidden="true">
        <span
          className={clsx(styles.fill, level)}
          style={{ transform: `scaleX(${String(Math.min(100, Math.max(0, value)) / 100)})` }}
        />
      </span>
    </div>
  );
}
