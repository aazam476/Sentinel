// SPDX-License-Identifier: Apache-2.0
import { useMachineAlerts } from '../../api/storage';
import { formatRelative } from '../../formatting/formatting';
import { useNow } from '../../hooks/useNow';
import alertStyles from '../Alerts/Alerts.module.css';
import { SeverityBadge } from '../Alerts/AlertsView';
import styles from './MachinePage.module.css';

/** MachineAlerts lists the machine's firing alerts, if it has any. */
export function MachineAlerts({ agentId }: { agentId: string }) {
  const alerts = useMachineAlerts(agentId);
  const now = useNow(60_000);
  const firing = (alerts.data?.alerts ?? []).filter((alert) => alert.state === 'firing');
  if (firing.length === 0) {
    return null;
  }
  return (
    <section className={styles.panel} aria-labelledby="machine-alerts-title">
      <h2 id="machine-alerts-title" className={styles.panelTitle}>
        Alerts
      </h2>
      <ul className={styles.alertList}>
        {firing.map((alert) => (
          <li key={alert.id} className={alertStyles.alertHeader}>
            <SeverityBadge alert={alert} />
            <span className={alertStyles.message}>{alert.message}</span>
            <span className={styles.muted}>
              since {formatRelative(alert.startedAt, now)}
              {alert.acknowledgedBy !== '' && ` · acknowledged by ${alert.acknowledgedBy}`}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
