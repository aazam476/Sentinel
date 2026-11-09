// SPDX-License-Identifier: Apache-2.0
import { Dialog } from '@base-ui/react/dialog';
import { useState } from 'react';

import type { SaveScheduleRequest, Schedule, Script } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import confirmStyles from '../../components/ConfirmDialog/ConfirmDialog.module.css';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import dialogStyles from '../../components/StepUpDialog/StepUpDialog.module.css';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from './Scripts.module.css';
import { describeCron } from './scriptText';
import { TargetPicker } from './TargetPicker';

interface ScheduleDialogProps {
  schedule?: Schedule;
  scripts: Script[];
  onClose: () => void;
  onSave: (
    request: SaveScheduleRequest & { id?: number },
    failed: (error: unknown) => void,
  ) => void;
}

/** browserZone is the time zone the browser is in, for new schedules. */
function browserZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

/** ScheduleDialog adds or changes a schedule, describing its cron in words. */
export function ScheduleDialog({ schedule, scripts, onClose, onSave }: ScheduleDialogProps) {
  // Only the fields a save sends: the server refuses any others.
  const [draft, setDraft] = useState<SaveScheduleRequest>(
    schedule === undefined
      ? {
          scriptId: scripts[0]?.id ?? 0,
          cron: '0 3 * * *',
          timezone: browserZone(),
          target: { kind: 'tag', value: '' },
          enabled: true,
        }
      : {
          scriptId: schedule.scriptId,
          cron: schedule.cron,
          timezone: schedule.timezone,
          target: schedule.target,
          enabled: schedule.enabled,
        },
  );
  const [error, setError] = useState<unknown>();
  const preview = describeCron(draft.cron);
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className={dialogStyles.backdrop} />
        <Dialog.Popup className={dialogStyles.popup}>
          <Dialog.Title className={dialogStyles.title}>
            {schedule === undefined ? 'New schedule' : 'Change schedule'}
          </Dialog.Title>
          <label className={styles.setting}>
            <span>Script</span>
            <select
              className={systemStyles.select}
              value={draft.scriptId}
              onChange={(event) => {
                setDraft({ ...draft, scriptId: Number(event.target.value) });
              }}
            >
              {scripts.map((script) => (
                <option key={script.id} value={script.id}>
                  {script.name}
                </option>
              ))}
            </select>
          </label>
          <Field
            label="When (cron)"
            value={draft.cron}
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            hint={preview ?? 'Five fields: minute, hour, day of month, month, day of week.'}
            onChange={(event) => {
              setDraft({ ...draft, cron: event.target.value });
            }}
          />
          <Field
            label="Time zone"
            value={draft.timezone}
            autoCapitalize="none"
            autoCorrect="off"
            onChange={(event) => {
              setDraft({ ...draft, timezone: event.target.value });
            }}
          />
          <TargetPicker
            target={draft.target}
            onChange={(target) => {
              setDraft({ ...draft, target });
            }}
          />
          <label className={styles.choice}>
            <input
              type="checkbox"
              checked={draft.enabled}
              onChange={(event) => {
                setDraft({ ...draft, enabled: event.target.checked });
              }}
            />
            On (unticked, the schedule is paused)
          </label>
          <FormError error={error} />
          <div className={confirmStyles.actions}>
            <Dialog.Close className={dialogStyles.close}>Cancel</Dialog.Close>
            <Button
              disabled={draft.target.value === '' || draft.scriptId === 0}
              onClick={() => {
                setError(undefined);
                onSave({ ...draft, id: schedule?.id }, setError);
              }}
            >
              Save
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
