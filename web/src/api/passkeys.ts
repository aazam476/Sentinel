// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest } from './client';
import { sessionQueryKey } from './session';
import type {
  PasskeyOptionsResponse,
  PasskeysResponse,
  SessionResponse,
  SignedInResponse,
} from './types.gen';
import { creationOptionsFromJSON, credentialToJSON, requestOptionsFromJSON } from './webauthn';

const passkeysQueryKey = ['passkeys'] as const;

/**
 * PasskeyCancelledError means the person closed the browser's passkey
 * prompt. Pages show nothing for it: closing the prompt is a choice, not a
 * failure.
 */
export class PasskeyCancelledError extends Error {
  constructor() {
    super('The passkey prompt was closed.');
    this.name = 'PasskeyCancelledError';
  }
}

async function askBrowser(run: () => Promise<Credential | null>): Promise<PublicKeyCredential> {
  try {
    const credential = await run();
    if (credential === null) {
      throw new PasskeyCancelledError();
    }
    return credential as PublicKeyCredential;
  } catch (error) {
    // Browsers report a closed or timed-out prompt as NotAllowedError.
    if (error instanceof DOMException && error.name === 'NotAllowedError') {
      throw new PasskeyCancelledError();
    }
    throw error;
  }
}

/**
 * registerPasskey adds a passkey for the current session. It returns the
 * signed-in response when this passkey completed a first-time enrollment,
 * and undefined when a signed-in user added another one.
 */
export async function registerPasskey(name: string): Promise<SignedInResponse | undefined> {
  const begun = await apiRequest<PasskeyOptionsResponse>('api/auth/passkeys/registration/begin', {
    method: 'POST',
  });
  const credential = await askBrowser(() =>
    navigator.credentials.create(creationOptionsFromJSON(begun.options)),
  );
  return apiRequest<SignedInResponse | undefined>('api/auth/passkeys/registration/finish', {
    method: 'POST',
    body: { name, credential: credentialToJSON(credential) },
  });
}

/**
 * verifyWithPasskey finishes signing in (for a password-only session) or
 * does a step-up check (for a signed-in one).
 */
export async function verifyWithPasskey(): Promise<SignedInResponse | SessionResponse> {
  const begun = await apiRequest<PasskeyOptionsResponse>('api/auth/passkeys/assertion/begin', {
    method: 'POST',
  });
  const credential = await askBrowser(() =>
    navigator.credentials.get(requestOptionsFromJSON(begun.options)),
  );
  return apiRequest<SignedInResponse | SessionResponse>('api/auth/passkeys/assertion/finish', {
    method: 'POST',
    body: { credential: credentialToJSON(credential) },
  });
}

/** sessionFrom picks the session out of either kind of response. */
export function sessionFrom(response: SignedInResponse | SessionResponse): SessionResponse {
  return 'session' in response ? response.session : response;
}

export function usePasskeys() {
  return useQuery({
    queryKey: passkeysQueryKey,
    queryFn: () => apiRequest<PasskeysResponse>('api/auth/passkeys'),
  });
}

export function useAddPasskey() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: registerPasskey,
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: passkeysQueryKey }),
        queryClient.invalidateQueries({ queryKey: sessionQueryKey }),
      ]),
  });
}

export function useRemovePasskey() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (passkeyId: number) =>
      apiRequest<undefined>(`api/auth/passkeys/${String(passkeyId)}`, { method: 'DELETE' }),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: passkeysQueryKey }),
        queryClient.invalidateQueries({ queryKey: sessionQueryKey }),
      ]),
  });
}

/** usePasskeyCheck runs a passkey sign-in or step-up and stores the session. */
export function usePasskeyCheck() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: verifyWithPasskey,
    onSuccess: (response) => {
      queryClient.setQueryData(sessionQueryKey, sessionFrom(response));
    },
  });
}
