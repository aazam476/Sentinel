// SPDX-License-Identifier: Apache-2.0
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';

import { isApiError } from '../../api/client';
import { useSecuritySettings } from '../../api/security';
import {
  sessionQueryKey,
  useBeginTOTPEnrollment,
  useConfirmTOTPEnrollment,
  useRemoveTOTP,
} from '../../api/session';
import type {
  SessionResponse,
  SignedInResponse,
  TOTPEnrollmentResponse,
} from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { RecoveryCodes } from '../../components/RecoveryCodes/RecoveryCodes';
import { StepUpDialog } from '../../components/StepUpDialog/StepUpDialog';
import { useStepUpRetry } from '../../components/StepUpDialog/useStepUpRetry';
import { TOTPSetup } from '../../components/TOTPSetup/TOTPSetup';
import styles from './AccountPage.module.css';

interface AuthenticatorSectionProps {
  session: SessionResponse;
}

/**
 * AuthenticatorSection adds or removes the user's authenticator app. Where
 * the server requires 2FA, the last second factor can't be removed.
 */
export function AuthenticatorSection({ session }: AuthenticatorSectionProps) {
  const queryClient = useQueryClient();
  const settings = useSecuritySettings();
  const begin = useBeginTOTPEnrollment();
  const confirm = useConfirmTOTPEnrollment();
  const removeTOTP = useRemoveTOTP();
  const stepUp = useStepUpRetry();
  const [enrollment, setEnrollment] = useState<TOTPEnrollmentResponse | null>(null);
  const [added, setAdded] = useState<SignedInResponse | null>(null);
  const [isConfirmingRemoval, setIsConfirmingRemoval] = useState(false);

  const start = () => {
    begin.mutate(undefined, {
      onSuccess: setEnrollment,
      onError: (error) => stepUp.handleError(error, start),
    });
  };
  const finish = (code: string) => {
    confirm.mutate(code, {
      onSuccess: (response) => {
        setEnrollment(null);
        setAdded(response);
      },
    });
  };
  const remove = () => {
    removeTOTP.mutate(undefined, {
      onSuccess: () => toast.success('Authenticator app removed'),
      onError: (error) => stepUp.handleError(error, remove),
    });
  };

  const required = settings.data?.requireTwoFactor === true;
  const canRemove = !required || session.secondFactors.passkey;
  const visibleError = (error: unknown) => (isApiError(error, 'step_up_required') ? null : error);

  return (
    <section className={styles.section} aria-labelledby="authenticator-title">
      <h2 id="authenticator-title" className={styles.sectionTitle}>
        Authenticator app
      </h2>
      {added !== null ? (
        <RecoveryCodes
          codes={added.recoveryCodes}
          onDone={() => {
            // The confirmation started a new session; the app moves onto it.
            queryClient.setQueryData(sessionQueryKey, added.session);
            setAdded(null);
            toast.success('Authenticator app added');
          }}
        />
      ) : enrollment !== null ? (
        <TOTPSetup
          enrollment={enrollment}
          isBusy={confirm.isPending}
          error={confirm.error}
          onConfirm={finish}
        />
      ) : session.secondFactors.totp ? (
        <>
          <p className={styles.sectionText}>
            {canRemove
              ? 'Signing in asks for a code from your authenticator app.'
              : 'Signing in asks for a code from your authenticator app. This server requires two-factor authentication, so add a passkey before removing it.'}
          </p>
          {canRemove && (
            <div>
              <Button
                variant="danger"
                isBusy={removeTOTP.isPending}
                onClick={() => {
                  setIsConfirmingRemoval(true);
                }}
              >
                Remove authenticator app
              </Button>
            </div>
          )}
        </>
      ) : (
        <>
          <p className={styles.sectionText}>
            Add one so a stolen password alone can’t open Sentinel: signing in will also ask for a
            code from the app.
          </p>
          <div>
            <Button variant="secondary" isBusy={begin.isPending} onClick={start}>
              Set up authenticator app
            </Button>
          </div>
        </>
      )}
      <FormError error={visibleError(begin.error) ?? visibleError(removeTOTP.error)} />
      <ConfirmDialog
        open={isConfirmingRemoval}
        onOpenChange={setIsConfirmingRemoval}
        title="Remove your authenticator app?"
        description={
          session.secondFactors.passkey
            ? 'Signing in will no longer ask for its codes. Your passkeys still work.'
            : 'Signing in will only ask for your password, and your recovery codes stop working.'
        }
        confirmLabel="Remove"
        onConfirm={remove}
      />
      <StepUpDialog
        open={stepUp.isOpen}
        onOpenChange={stepUp.setOpen}
        secondFactors={session.secondFactors}
        onConfirmed={stepUp.confirmed}
      />
    </section>
  );
}
