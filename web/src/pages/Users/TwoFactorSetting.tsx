// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { toast } from 'sonner';

import { useSecuritySettings, useUpdateSecuritySettings } from '../../api/security';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import styles from '../Scripts/Scripts.module.css';
import toggleStyles from './TwoFactorSetting.module.css';

interface TwoFactorSettingProps {
  /** withoutTwoFactor counts the users it would sign out. */
  withoutTwoFactor: number;
  /** onStepUp handles a refusal that asks for a fresh check, retrying after. */
  onStepUp: (error: unknown, again: () => void) => boolean;
}

/**
 * TwoFactorSetting is the admin's switch for requiring two-factor
 * authentication of every user (ADR 0039). Off by default.
 */
export function TwoFactorSetting({ withoutTwoFactor, onStepUp }: TwoFactorSettingProps) {
  const settings = useSecuritySettings();
  const update = useUpdateSecuritySettings();
  const [isConfirming, setIsConfirming] = useState(false);

  const save = (requireTwoFactor: boolean) => {
    update.mutate(
      { requireTwoFactor },
      {
        onSuccess: () =>
          toast.success(
            requireTwoFactor
              ? 'Two-factor authentication is now required'
              : 'Two-factor authentication is now optional',
          ),
        onError: (error) => {
          if (
            !onStepUp(error, () => {
              save(requireTwoFactor);
            })
          ) {
            toast.error(messageFor(error));
          }
        },
      },
    );
  };

  const required = settings.data?.requireTwoFactor === true;
  return (
    <section className={styles.section} aria-labelledby="two-factor-title">
      <h2 id="two-factor-title" className={styles.subtitle}>
        Two-factor authentication
      </h2>
      <label className={toggleStyles.toggle}>
        <input
          type="checkbox"
          checked={required}
          disabled={settings.data === undefined || update.isPending}
          onChange={(event) => {
            if (event.target.checked) {
              setIsConfirming(true);
            } else {
              save(false);
            }
          }}
        />
        <span>Require it for everyone</span>
      </label>
      <p className={styles.muted}>
        {required
          ? 'Everyone signs in with a code from an authenticator app or a passkey.'
          : 'Each user chooses whether to add an authenticator app or a passkey from their account.'}
      </p>
      <FormError error={settings.error} />
      <ConfirmDialog
        open={isConfirming}
        onOpenChange={setIsConfirming}
        title="Require two-factor authentication?"
        description={
          withoutTwoFactor === 0
            ? 'Everyone already uses it, so no one is signed out.'
            : `${String(withoutTwoFactor)} ${withoutTwoFactor === 1 ? 'user' : 'users'} without it will be signed out, and set it up the next time they sign in.`
        }
        confirmLabel="Require it"
        onConfirm={() => {
          save(true);
        }}
      />
    </section>
  );
}
