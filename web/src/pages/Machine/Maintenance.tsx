// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { toast } from 'sonner';

import { useSetMaintenance } from '../../api/alerts';
import type { AgentDetails } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { FormError } from '../../components/FormError/FormError';
import { formatDateTime } from '../../formatting/formatting';
import styles from './MachinePage.module.css';

const windows = [
  { hours: 1, label: '1 hour' },
  { hours: 4, label: '4 hours' },
  { hours: 24, label: '1 day' },
  { hours: 24 * 7, label: '1 week' },
];

/**
 * Maintenance lets an admin silence a machine's notifications for a while,
 * during planned work. Its alerts still show; nobody is sent them.
 */
export function Maintenance({ agent }: { agent: AgentDetails }) {
  const setMaintenance = useSetMaintenance(agent.id);
  const [hours, setHours] = useState(4);
  const until = agent.maintenanceUntil;
  return (
    <section className={styles.panel} aria-labelledby="maintenance-title">
      <h2 id="maintenance-title" className={styles.panelTitle}>
        Maintenance
      </h2>
      {until !== null ? (
        <>
          <p className={styles.muted}>
            In maintenance until {formatDateTime(until)}. Alerts still show here, but nobody is
            notified.
          </p>
          <div className={styles.settingsForm}>
            <Button
              variant="secondary"
              isBusy={setMaintenance.isPending}
              onClick={() => {
                setMaintenance.mutate(null, {
                  onSuccess: () => toast.success('Maintenance ended'),
                });
              }}
            >
              End maintenance
            </Button>
          </div>
        </>
      ) : (
        <>
          <p className={styles.muted}>
            Silence this machine’s notifications during planned work. Alerts still show here.
          </p>
          <div className={styles.settingsForm}>
            <label className={styles.field}>
              <span className={styles.label}>For</span>
              <select
                className={styles.select}
                value={hours}
                onChange={(event) => {
                  setHours(Number(event.target.value));
                }}
              >
                {windows.map((window) => (
                  <option key={window.hours} value={window.hours}>
                    {window.label}
                  </option>
                ))}
              </select>
            </label>
            <Button
              variant="secondary"
              isBusy={setMaintenance.isPending}
              onClick={() => {
                setMaintenance.mutate(Date.now() + hours * 3_600_000, {
                  onSuccess: () => toast.success('Maintenance started'),
                });
              }}
            >
              Start maintenance
            </Button>
          </div>
        </>
      )}
      <FormError error={setMaintenance.error} />
    </section>
  );
}
