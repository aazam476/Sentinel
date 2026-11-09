// SPDX-License-Identifier: Apache-2.0
import { Dialog } from '@base-ui/react/dialog';
import { type SubmitEvent, useState } from 'react';

import type { CreateUserRequest } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import confirmStyles from '../../components/ConfirmDialog/ConfirmDialog.module.css';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import dialogStyles from '../../components/StepUpDialog/StepUpDialog.module.css';
import alertStyles from '../Alerts/Alerts.module.css';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from '../Scripts/Scripts.module.css';
import { roles } from './roles';

interface AddUserDialogProps {
  onClose: () => void;
  onAdd: (request: CreateUserRequest, failed: (error: unknown) => void) => void;
  isBusy: boolean;
}

/** AddUserDialog creates a user with a first password and a role. */
export function AddUserDialog({ onClose, onAdd, isBusy }: AddUserDialogProps) {
  const [draft, setDraft] = useState<CreateUserRequest>({
    username: '',
    password: '',
    role: 'viewer',
  });
  const [error, setError] = useState<unknown>();
  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError(undefined);
    onAdd({ ...draft, username: draft.username.trim() }, setError);
  };
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
          <Dialog.Title className={dialogStyles.title}>Add a user</Dialog.Title>
          <form className={alertStyles.fields} onSubmit={submit}>
            <Field
              label="Username"
              value={draft.username}
              autoCapitalize="none"
              autoCorrect="off"
              autoComplete="off"
              maxLength={64}
              onChange={(event) => {
                setDraft({ ...draft, username: event.target.value });
              }}
            />
            <Field
              label="First password"
              type="password"
              autoComplete="new-password"
              hint="At least 12 characters. Give it to them privately; they set up two-factor authentication when they first sign in."
              value={draft.password}
              onChange={(event) => {
                setDraft({ ...draft, password: event.target.value });
              }}
            />
            <label className={styles.setting}>
              <span>Role</span>
              <select
                className={systemStyles.select}
                value={draft.role}
                onChange={(event) => {
                  setDraft({ ...draft, role: event.target.value });
                }}
              >
                {roles.map((role) => (
                  <option key={role.value} value={role.value}>
                    {role.label}
                  </option>
                ))}
              </select>
            </label>
            <p className={styles.muted}>
              {roles.find((role) => role.value === draft.role)?.description}
            </p>
            <FormError error={error} />
            <div className={confirmStyles.actions}>
              <Dialog.Close className={dialogStyles.close}>Cancel</Dialog.Close>
              <Button
                type="submit"
                isBusy={isBusy}
                disabled={draft.username.trim() === '' || draft.password === ''}
              >
                Add user
              </Button>
            </div>
          </form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
