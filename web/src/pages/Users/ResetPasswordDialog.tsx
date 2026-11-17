// SPDX-License-Identifier: Apache-2.0
import { Dialog } from '@base-ui/react/dialog';
import { type SubmitEvent, useState } from 'react';

import type { UserResponse } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import confirmStyles from '../../components/ConfirmDialog/ConfirmDialog.module.css';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import dialogStyles from '../../components/StepUpDialog/StepUpDialog.module.css';
import alertStyles from '../Alerts/Alerts.module.css';

interface ResetPasswordDialogProps {
  user: UserResponse;
  isBusy: boolean;
  onClose: () => void;
  onReset: (password: string, failed: (error: unknown) => void) => void;
}

/** ResetPasswordDialog gives a user who forgot theirs a new password. */
export function ResetPasswordDialog({ user, isBusy, onClose, onReset }: ResetPasswordDialogProps) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState<unknown>();
  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError(undefined);
    onReset(password, setError);
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
          <Dialog.Title className={dialogStyles.title}>
            Reset {user.username}’s password
          </Dialog.Title>
          <Dialog.Description className={dialogStyles.description}>
            They are signed out everywhere. Give them the new password privately; they can change it
            on their Account page.
          </Dialog.Description>
          <form className={alertStyles.fields} onSubmit={submit}>
            <Field
              label="New password"
              type="password"
              autoComplete="new-password"
              hint="At least 12 characters."
              value={password}
              onChange={(event) => {
                setPassword(event.target.value);
              }}
            />
            <FormError error={error} />
            <div className={confirmStyles.actions}>
              <Dialog.Close className={dialogStyles.close}>Cancel</Dialog.Close>
              <Button type="submit" isBusy={isBusy} disabled={password === ''}>
                Reset password
              </Button>
            </div>
          </form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
