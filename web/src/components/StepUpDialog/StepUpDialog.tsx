// SPDX-License-Identifier: Apache-2.0
import { Dialog } from '@base-ui/react/dialog';
import { useState, type SubmitEvent } from 'react';

import { PasskeyCancelledError, usePasskeyCheck } from '../../api/passkeys';
import { useStepUp } from '../../api/session';
import type { SecondFactors } from '../../api/types.gen';
import { Button } from '../Button/Button';
import { CodeField } from '../Field/CodeField';
import { Field } from '../Field/Field';
import { FormError } from '../FormError/FormError';
import styles from './StepUpDialog.module.css';

interface StepUpDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  secondFactors: SecondFactors;
  /** onConfirmed runs after a successful check, to retry the action. */
  onConfirmed: () => void;
}

/**
 * StepUpDialog asks for a fresh second-factor check before a sensitive
 * action, when the last one was more than 15 minutes ago.
 */
export function StepUpDialog({
  open,
  onOpenChange,
  secondFactors,
  onConfirmed,
}: StepUpDialogProps) {
  const [code, setCode] = useState('');
  const stepUp = useStepUp();
  const passkeyCheck = usePasskeyCheck();

  const finish = () => {
    setCode('');
    onOpenChange(false);
    onConfirmed();
  };

  const hasSecondFactor = secondFactors.totp || secondFactors.passkey;

  const handleSubmit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    const request = hasSecondFactor ? { totpCode: code.trim() } : { password: code };
    stepUp.mutate(request, { onSuccess: finish });
  };

  const passkeyError =
    passkeyCheck.error instanceof PasskeyCancelledError ? null : passkeyCheck.error;

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className={styles.backdrop} />
        <Dialog.Popup className={styles.popup}>
          <Dialog.Title className={styles.title}>Confirm it’s you</Dialog.Title>
          <Dialog.Description className={styles.description}>
            {hasSecondFactor
              ? 'This change needs a fresh check of your second factor.'
              : 'This change needs your password again.'}
          </Dialog.Description>
          {secondFactors.passkey && (
            <Button
              variant={secondFactors.totp ? 'secondary' : 'primary'}
              isFullWidth
              isBusy={passkeyCheck.isPending}
              onClick={() => {
                passkeyCheck.mutate(undefined, { onSuccess: finish });
              }}
            >
              Use a passkey
            </Button>
          )}
          <FormError error={passkeyError} />
          {secondFactors.totp && (
            <form className={styles.form} onSubmit={handleSubmit}>
              <CodeField
                value={code}
                onChange={(event) => {
                  setCode(event.target.value);
                }}
              />
              <FormError error={stepUp.error} />
              <Button type="submit" isFullWidth isBusy={stepUp.isPending}>
                Confirm
              </Button>
            </form>
          )}
          {!hasSecondFactor && (
            <form className={styles.form} onSubmit={handleSubmit}>
              <Field
                label="Password"
                type="password"
                value={code}
                onChange={(event) => {
                  setCode(event.target.value);
                }}
                autoComplete="current-password"
                autoFocus
                required
              />
              <FormError error={stepUp.error} />
              <Button type="submit" isFullWidth isBusy={stepUp.isPending}>
                Confirm
              </Button>
            </form>
          )}
          <Dialog.Close className={styles.close}>Cancel</Dialog.Close>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
