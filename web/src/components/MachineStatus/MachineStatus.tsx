// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';

import styles from './MachineStatus.module.css';

interface MachineStatusProps {
  isOnline: boolean;
  /** isCompact shows only the dot, with the status for screen readers. */
  isCompact?: boolean;
}

/**
 * MachineStatus shows whether a machine is connected. Color is never the
 * only signal: the word is always there, visibly or for screen readers.
 */
export function MachineStatus({ isOnline, isCompact = false }: MachineStatusProps) {
  const label = isOnline ? 'Online' : 'Offline';
  return (
    <span className={clsx(styles.status, isOnline ? styles.online : styles.offline)}>
      <span className={styles.dot} aria-hidden="true" />
      <span className={clsx(isCompact && styles.visuallyHidden)}>{label}</span>
    </span>
  );
}
