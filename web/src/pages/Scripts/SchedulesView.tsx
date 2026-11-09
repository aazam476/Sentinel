// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { toast } from 'sonner';

import { useAgents } from '../../api/agents';
import { useDeleteSchedule, useSaveSchedule, useSchedules, useScripts } from '../../api/scripts';
import type { Schedule } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import { formatDateTime } from '../../formatting/formatting';
import systemStyles from '../Machine/SystemTab.module.css';
import { ScheduleDialog } from './ScheduleDialog';
import styles from './Scripts.module.css';
import { describeCron, describeTarget } from './scriptText';

/** SchedulesView lists the schedules, soonest first, and edits them. */
export function SchedulesView() {
  const schedules = useSchedules();
  const scripts = useScripts();
  const agents = useAgents();
  const save = useSaveSchedule();
  const remove = useDeleteSchedule();
  const [editing, setEditing] = useState<Schedule | 'new'>();
  const [deleting, setDeleting] = useState<Schedule>();
  const names = new Map((agents.data?.agents ?? []).map((agent) => [agent.id, agent.name]));
  const noScripts = scripts.data?.scripts.length === 0;
  return (
    <section className={styles.section} aria-label="Schedules">
      <div className={systemStyles.toolbar}>
        <Button
          disabled={noScripts}
          onClick={() => {
            setEditing('new');
          }}
        >
          New schedule
        </Button>
        {noScripts && <span className={styles.muted}>Write a script first.</span>}
      </div>
      {schedules.isPending && <p className={styles.muted}>Loading schedules…</p>}
      {schedules.isError && <FormError error={schedules.error} />}
      {schedules.data?.schedules.length === 0 && (
        <p className={styles.muted}>Nothing is scheduled.</p>
      )}
      {schedules.data !== undefined && schedules.data.schedules.length > 0 && (
        <ul className={systemStyles.list}>
          {schedules.data.schedules.map((schedule) => (
            <li
              key={schedule.id}
              className={styles.row}
              aria-label={`Schedule of ${schedule.scriptName}`}
            >
              <span className={styles.runHeader}>
                <button
                  type="button"
                  className={systemStyles.linkButton}
                  onClick={() => {
                    setEditing(schedule);
                  }}
                >
                  <span className={styles.name}>{schedule.scriptName}</span>
                </button>
                {!schedule.enabled && <span className={systemStyles.badge}>Paused</span>}
              </span>
              <span className={systemStyles.meta}>
                <span>{describeCron(schedule.cron) ?? schedule.cron}</span>
                <span>{schedule.timezone}</span>
                <span>{describeTarget(schedule.target, names)}</span>
              </span>
              <span className={systemStyles.meta}>
                {schedule.nextRunAt !== null && (
                  <span>Next {formatDateTime(schedule.nextRunAt)}</span>
                )}
                {schedule.lastRunAt !== null && (
                  <span>Last {formatDateTime(schedule.lastRunAt)}</span>
                )}
                <button
                  type="button"
                  className={systemStyles.linkButton}
                  onClick={() => {
                    setDeleting(schedule);
                  }}
                >
                  Delete
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
      {editing !== undefined && (
        <ScheduleDialog
          schedule={editing === 'new' ? undefined : editing}
          scripts={scripts.data?.scripts ?? []}
          onClose={() => {
            setEditing(undefined);
          }}
          onSave={(request, done) => {
            save.mutate(request, {
              onSuccess: () => {
                toast.success('Schedule saved');
                setEditing(undefined);
              },
              onError: done,
            });
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== undefined}
        onOpenChange={(isOpen) => {
          if (!isOpen) {
            setDeleting(undefined);
          }
        }}
        title="Delete this schedule?"
        description="The script stays in the library, and past runs are kept."
        confirmLabel="Delete"
        onConfirm={() => {
          if (deleting !== undefined) {
            remove.mutate(deleting.id, {
              onSuccess: () => toast.success('Schedule deleted'),
              onError: (error) => toast.error(messageFor(error)),
            });
          }
        }}
      />
    </section>
  );
}
