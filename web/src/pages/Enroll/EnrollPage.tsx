// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState, type SubmitEvent } from 'react';

import { PasskeyCancelledError, registerPasskey } from '../../api/passkeys';
import {
  sessionQueryKey,
  useBeginTOTPEnrollment,
  useConfirmTOTPEnrollment,
  useLogout,
} from '../../api/session';
import type { SignedInResponse } from '../../api/types.gen';
import { passkeysSupported } from '../../api/webauthn';
import { AuthLayout } from '../../components/AuthLayout/AuthLayout';
import { Button } from '../../components/Button/Button';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import { RecoveryCodes } from '../../components/RecoveryCodes/RecoveryCodes';
import { TOTPSetup } from '../../components/TOTPSetup/TOTPSetup';
import styles from './EnrollPage.module.css';

/**
 * EnrollPage is where a user without a second factor sets one up before
 * they can use Sentinel (SEC-04): an authenticator app or a passkey.
 */
export function EnrollPage() {
  const queryClient = useQueryClient();
  const beginTOTP = useBeginTOTPEnrollment();
  const confirmTOTP = useConfirmTOTPEnrollment();
  const addPasskey = useMutation({ mutationFn: registerPasskey });
  const logout = useLogout();
  const [signedIn, setSignedIn] = useState<SignedInResponse | null>(null);
  const [passkeyName, setPasskeyName] = useState('');

  const handlePasskey = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    addPasskey.mutate(passkeyName.trim(), {
      onSuccess: (response) => {
        if (response !== undefined) {
          setSignedIn(response);
        }
      },
    });
  };

  if (signedIn !== null) {
    return (
      <AuthLayout title="You’re all set">
        <RecoveryCodes
          codes={signedIn.recoveryCodes}
          onDone={() => {
            queryClient.setQueryData(sessionQueryKey, signedIn.session);
          }}
        />
      </AuthLayout>
    );
  }
  if (beginTOTP.data !== undefined) {
    return (
      <AuthLayout title="Set up your authenticator app">
        <TOTPSetup
          enrollment={beginTOTP.data}
          isBusy={confirmTOTP.isPending}
          error={confirmTOTP.error}
          onConfirm={(code) => {
            confirmTOTP.mutate(code, { onSuccess: setSignedIn });
          }}
        />
      </AuthLayout>
    );
  }

  const passkeyError = addPasskey.error instanceof PasskeyCancelledError ? null : addPasskey.error;
  return (
    <AuthLayout
      title="Set up two-factor authentication"
      description={<p>Every Sentinel account needs a second factor. Choose one to continue.</p>}
    >
      <section className={styles.option} aria-labelledby="totp-option">
        <h2 id="totp-option" className={styles.optionTitle}>
          Authenticator app
        </h2>
        <p className={styles.optionText}>
          Codes from an app such as 1Password, Bitwarden or Google Authenticator.
        </p>
        <Button
          isFullWidth
          isBusy={beginTOTP.isPending}
          onClick={() => {
            beginTOTP.mutate();
          }}
        >
          Use an authenticator app
        </Button>
        <FormError error={beginTOTP.error} />
      </section>
      {passkeysSupported() && (
        <form className={styles.option} aria-labelledby="passkey-option" onSubmit={handlePasskey}>
          <h2 id="passkey-option" className={styles.optionTitle}>
            Passkey
          </h2>
          <p className={styles.optionText}>
            Your device’s fingerprint, face or PIN, or a security key.
          </p>
          <Field
            label="Name for this passkey"
            value={passkeyName}
            onChange={(event) => {
              setPasskeyName(event.target.value);
            }}
            hint="For example “Work laptop”. Optional."
            maxLength={64}
          />
          <Button type="submit" variant="secondary" isFullWidth isBusy={addPasskey.isPending}>
            Use a passkey
          </Button>
          <FormError error={passkeyError} />
        </form>
      )}
      <Button
        variant="ghost"
        onClick={() => {
          logout.mutate();
        }}
      >
        Cancel and sign out
      </Button>
    </AuthLayout>
  );
}
