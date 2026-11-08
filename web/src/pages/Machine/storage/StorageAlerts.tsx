// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';

import { useMachineAlerts } from '../../../api/storage';
import { formatRelative } from '../../../formatting/formatting';
import { useNow } from '../../../hooks/useNow';
import systemStyles from '../SystemTab.module.css';
import styles from './Storage.module.css';
import { isStorageAlert } from './storageText';

/** StorageAlerts lists the machine's firing storage alerts, if any. */
export function StorageAlerts({ agentId }: { agentId: string }) {
  const alerts = useMachineAlerts(agentId);
  const now = useNow(60_000);
  const firing = (alerts.data?.alerts ?? []).filter(
    (alert) => alert.state === 'firing' && isStorageAlert(alert.kind),
  );
  if (firing.length === 0) {
    return null;
  }
  return (
    <section className={styles.alerts} aria-labelledby="storage-alerts-title">
      <h2 id="storage-alerts-title" className={styles.title}>
        Storage alerts
      </h2>
      <ul className={styles.alertList}>
        {firing.map((alert) => (
          <li key={alert.id} className={styles.alert}>
            <span
              className={clsx(
                systemStyles.badge,
                alert.severity === 'critical' ? systemStyles.failed : systemStyles.warning,
              )}
            >
              {alert.severity === 'critical' ? 'Critical' : 'Warning'}
            </span>
            <span className={styles.alertMessage}>{alert.message}</span>
            <span className={styles.muted}>since {formatRelative(alert.startedAt, now)}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
