// SPDX-License-Identifier: Apache-2.0
import { useState, type SubmitEvent } from 'react';
import { toast } from 'sonner';

import { PasskeyCancelledError, usePasskeyCheck } from '../../api/passkeys';
import { useLogout, useSecondFactor } from '../../api/session';
import type { SessionResponse, SignedInResponse } from '../../api/types.gen';
import { passkeysSupported } from '../../api/webauthn';
import { AuthLayout } from '../../components/AuthLayout/AuthLayout';
import { Button } from '../../components/Button/Button';
import { CodeField } from '../../components/Field/CodeField';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import { countOf } from '../../formatting/formatting';
import styles from './LoginPage.module.css';

interface SecondFactorPageProps {
  session: SessionResponse;
}

/**
 * SecondFactorPage finishes signing in with a passkey, an authenticator
 * code or a recovery code (SEC-04).
 */
export function SecondFactorPage({ session }: SecondFactorPageProps) {
  const secondFactor = useSecondFactor();
  const passkeyCheck = usePasskeyCheck();
  const logout = useLogout();
  const [isUsingRecoveryCode, setIsUsingRecoveryCode] = useState(
    !session.secondFactors.totp && !session.secondFactors.passkey,
  );
  const [code, setCode] = useState('');

  const handleSignedIn = (response: SignedInResponse) => {
    if (response.remainingRecoveryCodes !== null) {
      toast.warning(
        `${countOf(response.remainingRecoveryCodes, 'recovery code', 'recovery codes')} left`,
        {
          description: 'Each code works once. Set up your authenticator again from Account.',
        },
      );
    }
  };

  const handleSubmit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    const request = isUsingRecoveryCode ? { recoveryCode: code.trim() } : { totpCode: code.trim() };
    secondFactor.mutate(request, { onSuccess: handleSignedIn });
  };

  const switchMethod = () => {
    setIsUsingRecoveryCode(!isUsingRecoveryCode);
    setCode('');
    secondFactor.reset();
  };

  const canUsePasskey = session.secondFactors.passkey && passkeysSupported();
  const passkeyError =
    passkeyCheck.error instanceof PasskeyCancelledError ? null : passkeyCheck.error;
  const showCodeForm = isUsingRecoveryCode || session.secondFactors.totp;

  return (
    <AuthLayout
      title="Confirm it’s you"
      description={<p>Signing in as {session.user.username}.</p>}
    >
      {canUsePasskey && !isUsingRecoveryCode && (
        <>
          <Button
            isFullWidth
            variant={session.secondFactors.totp ? 'secondary' : 'primary'}
            isBusy={passkeyCheck.isPending}
            onClick={() => {
              passkeyCheck.mutate();
            }}
          >
            Use a passkey
          </Button>
          <FormError error={passkeyError} />
        </>
      )}
      {showCodeForm && (
        <form className={styles.form} onSubmit={handleSubmit}>
          {isUsingRecoveryCode ? (
            <Field
              label="Recovery code"
              value={code}
              onChange={(event) => {
                setCode(event.target.value);
              }}
              hint="One of the codes you saved when you set up two-factor authentication."
              autoComplete="off"
              autoCapitalize="characters"
              autoCorrect="off"
              spellCheck={false}
              required
              autoFocus
            />
          ) : (
            <CodeField
              value={code}
              onChange={(event) => {
                setCode(event.target.value);
              }}
              autoFocus
            />
          )}
          <FormError error={secondFactor.error} />
          <Button type="submit" isFullWidth isBusy={secondFactor.isPending}>
            Verify
          </Button>
        </form>
      )}
      <div className={styles.links}>
        <Button variant="ghost" onClick={switchMethod}>
          {isUsingRecoveryCode ? 'Use your authenticator instead' : 'Use a recovery code'}
        </Button>
        <Button
          variant="ghost"
          onClick={() => {
            logout.mutate();
          }}
        >
          Cancel
        </Button>
      </div>
    </AuthLayout>
  );
}
