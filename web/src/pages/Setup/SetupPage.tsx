// SPDX-License-Identifier: Apache-2.0
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { sessionQueryKey } from '../../api/session';
import { setupStatusQueryKey, useBeginSetup, useCompleteSetup } from '../../api/setup';
import type { BeginSetupResponse, SignedInResponse } from '../../api/types.gen';
import { AuthLayout } from '../../components/AuthLayout/AuthLayout';
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
  const handleCodesSaved = () => {
    if (signedIn === null) {
      return;
    }
    queryClient.setQueryData(setupStatusQueryKey, { setupRequired: false });
    queryClient.setQueryData(sessionQueryKey, signedIn.session);
  };

  if (signedIn !== null) {
    return (
      <AuthLayout title="Sentinel is ready">
        <RecoveryCodes codes={signedIn.recoveryCodes} onDone={handleCodesSaved} />
      </AuthLayout>
    );
  }
  if (setup !== null) {
    return (
      <AuthLayout
        title="Set up two-factor authentication"
        description={<p>Every account needs a second factor.</p>}
      >
        <TOTPSetup
          enrollment={setup.totp}
          isBusy={completeSetup.isPending}
          error={completeSetup.error}
          onConfirm={handleCode}
        />
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
