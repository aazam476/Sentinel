// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { Link, useOutletContext } from 'react-router';
import { toast } from 'sonner';

import { useAcknowledgeAlert, useAlerts } from '../../api/alerts';
import type { FleetAlert, SessionResponse } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import { formatDateTime, formatRelative } from '../../formatting/formatting';
import { useNow } from '../../hooks/useNow';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from '../Scripts/Scripts.module.css';
import alertStyles from './Alerts.module.css';

/** AlertsView lists every firing alert, then the recently resolved ones. */
export function AlertsView() {
  const session = useOutletContext<SessionResponse>();
  const alerts = useAlerts();
  const now = useNow(60_000);
  const list = alerts.data?.alerts ?? [];
  const firing = list.filter((alert) => alert.state === 'firing');
  const resolved = list.filter((alert) => alert.state !== 'firing');
  return (
    <section className={styles.section} aria-label="Alerts">
      {alerts.isPending && <p className={styles.muted}>Loading alerts…</p>}
      {alerts.isError && <FormError error={alerts.error} />}
      {alerts.data !== undefined && (
        <>
          <h2 className={alertStyles.heading}>Firing</h2>
          {firing.length === 0 ? (
            <p className={styles.muted}>Nothing needs attention.</p>
          ) : (
            <AlertList
              alerts={firing}
              now={now}
              canAcknowledge={session.user.role === 'admin'}
              label="Firing alerts"
            />
          )}
          <h2 className={alertStyles.heading}>Recently resolved</h2>
          {resolved.length === 0 ? (
            <p className={styles.muted}>No alerts have resolved yet.</p>
          ) : (
            <AlertList alerts={resolved} now={now} canAcknowledge={false} label="Resolved alerts" />
          )}
        </>
      )}
    </section>
  );
}

interface AlertListProps {
  alerts: FleetAlert[];
  now: number;
  canAcknowledge: boolean;
  label: string;
}

function AlertList({ alerts, now, canAcknowledge, label }: AlertListProps) {
  const acknowledge = useAcknowledgeAlert();
  return (
    <ul className={systemStyles.list} aria-label={label}>
      {alerts.map((alert) => (
        <li key={alert.id} className={styles.row} aria-label={alert.message}>
          <span className={alertStyles.alertHeader}>
            <SeverityBadge alert={alert} />
            <span className={alertStyles.message}>{alert.message}</span>
          </span>
          <span className={systemStyles.meta}>
            <Link to={`/machines/${encodeURIComponent(alert.agentId)}`} title={alert.agentName}>
              {alert.agentName}
            </Link>
            {alert.ruleName !== '' && <span>{alert.ruleName}</span>}
            <span title={formatDateTime(alert.startedAt)}>
              Started {formatRelative(alert.startedAt, now)}
            </span>
            {alert.resolvedAt !== null && (
              <span title={formatDateTime(alert.resolvedAt)}>
                Resolved {formatRelative(alert.resolvedAt, now)}
              </span>
            )}
          </span>
          {alert.state === 'firing' &&
            (alert.acknowledgedBy !== '' ? (
              <span className={styles.muted}>
                Acknowledged by {alert.acknowledgedBy}
                {alert.acknowledgedAt !== null && ` ${formatRelative(alert.acknowledgedAt, now)}`}
              </span>
            ) : (
              canAcknowledge && (
                <span className={alertStyles.actions}>
                  <Button
                    variant="secondary"
                    isBusy={acknowledge.isPending && acknowledge.variables === alert.id}
                    onClick={() => {
                      acknowledge.mutate(alert.id, {
                        onSuccess: () => toast.success('Acknowledged'),
                        onError: (error) => toast.error(messageFor(error)),
                      });
                    }}
                  >
                    Acknowledge
                  </Button>
                </span>
              )
            ))}
        </li>
      ))}
    </ul>
  );
}

/** SeverityBadge shows an alert's severity, or that it resolved. */
export function SeverityBadge({ alert }: { alert: { severity: string; state: string } }) {
  if (alert.state !== 'firing') {
    return <span className={clsx(systemStyles.badge, systemStyles.running)}>Resolved</span>;
  }
  const isCritical = alert.severity === 'critical';
  return (
    <span
      className={clsx(systemStyles.badge, isCritical ? systemStyles.failed : systemStyles.warning)}
    >
      {isCritical ? 'Critical' : 'Warning'}
    </span>
  );
}
