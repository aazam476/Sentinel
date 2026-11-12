// SPDX-License-Identifier: Apache-2.0
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { sessionQueryKey } from '../../api/session';
import { setupStatusQueryKey, useBeginSetup, useCompleteSetup } from '../../api/setup';
import type { BeginSetupResponse, SignedInResponse } from '../../api/types.gen';
import { AuthLayout } from '../../components/AuthLayout/AuthLayout';
import { Button } from '../../components/Button/Button';
import { RecoveryCodes } from '../../components/RecoveryCodes/RecoveryCodes';
import { TOTPSetup } from '../../components/TOTPSetup/TOTPSetup';
import { SetupDetailsForm, type SetupDetails } from './SetupDetailsForm';

/**
 * SetupPage creates the first admin on a fresh install: username and
 * password, then an authenticator app, then recovery codes.
 */
export function SetupPage() {
  const queryClient = useQueryClient();
  const beginSetup = useBeginSetup();
  const completeSetup = useCompleteSetup();
  const [setup, setSetup] = useState<BeginSetupResponse | null>(null);
  const [signedIn, setSignedIn] = useState<SignedInResponse | null>(null);

  const handleDetails = (submitted: SetupDetails) => {
    beginSetup.mutate(submitted, {
      onSuccess: (response) => {
        setSetup(response);
      },
    });
  };

  const handleCode = (totpCode: string) => {
    if (setup === null) {
      return;
    }
    completeSetup.mutate({ setupId: setup.setupId, totpCode }, { onSuccess: setSignedIn });
  };

  // The session is stored only after the codes are saved: storing it sends
  // the browser into the app.
  const enter = (finished: SignedInResponse) => {
    queryClient.setQueryData(setupStatusQueryKey, { setupRequired: false });
    queryClient.setQueryData(sessionQueryKey, finished.session);
  };

  // Without 2FA there are no recovery codes to save: straight in.
  const handleSkip = () => {
    if (setup === null) {
      return;
    }
    completeSetup.mutate(
      { setupId: setup.setupId, totpCode: '', skipTwoFactor: true },
      { onSuccess: enter },
    );
  };

  if (signedIn !== null) {
    return (
      <AuthLayout title="Sentinel is ready">
        <RecoveryCodes
          codes={signedIn.recoveryCodes}
          onDone={() => {
            enter(signedIn);
          }}
        />
      </AuthLayout>
    );
  }
  if (setup !== null) {
    return (
      <AuthLayout
        title="Set up two-factor authentication"
        description={
          <p>
            Recommended: with an authenticator app, a stolen password alone can’t open Sentinel. You
            can also add one later from your account.
          </p>
        }
      >
        <TOTPSetup
          enrollment={setup.totp}
          isBusy={completeSetup.isPending}
          error={completeSetup.error}
          onConfirm={handleCode}
        />
        <Button variant="secondary" isFullWidth onClick={handleSkip}>
          Skip for now
        </Button>
      </AuthLayout>
    );
  }
  return (
    <AuthLayout
      title="Set up Sentinel"
      description={
        <p>
          Create the first admin account. Whoever finishes this first owns the server, so do it
          before anyone else can reach this page.
        </p>
      }
    >
      <SetupDetailsForm
        isBusy={beginSetup.isPending}
        error={beginSetup.error}
        onSubmit={handleDetails}
      />
    </AuthLayout>
  );
}
