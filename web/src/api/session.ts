// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest, isApiError } from './client';
import type {
  LoginRequest,
  LoginResponse,
  SecondFactorRequest,
  SessionResponse,
  SignedInResponse,
  StepUpRequest,
  TOTPEnrollmentResponse,
} from './types.gen';

export const sessionQueryKey = ['session'] as const;

/**
 * useSession returns the current session, or null when the browser isn't
 * signed in at all.
 */
export function useSession() {
  return useQuery({
    queryKey: sessionQueryKey,
    queryFn: async (): Promise<SessionResponse | null> => {
      try {
        return await apiRequest<SessionResponse>('api/auth/session');
      } catch (error) {
        if (isApiError(error, 'not_authenticated')) {
          return null;
        }
        throw error;
      }
    },
  });
}

/** useSessionUpdater returns a function that replaces the cached session. */
function useSessionUpdater() {
  const queryClient = useQueryClient();
  return (session: SessionResponse | null) => {
    queryClient.setQueryData(sessionQueryKey, session);
  };
}

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (request: LoginRequest) =>
      apiRequest<LoginResponse>('api/auth/login', { method: 'POST', body: request }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: sessionQueryKey }),
  });
}

export function useSecondFactor() {
  const setSession = useSessionUpdater();
  return useMutation({
    mutationFn: (request: SecondFactorRequest) =>
      apiRequest<SignedInResponse>('api/auth/login/second-factor', {
        method: 'POST',
        body: request,
      }),
    onSuccess: (response) => {
      setSession(response.session);
    },
  });
}

export function useBeginTOTPEnrollment() {
  return useMutation({
    mutationFn: () =>
      apiRequest<TOTPEnrollmentResponse>('api/auth/totp/enrollment', { method: 'POST' }),
  });
}

export function useConfirmTOTPEnrollment() {
  return useMutation({
    mutationFn: (totpCode: string) =>
      apiRequest<SignedInResponse>('api/auth/totp/enrollment/confirm', {
        method: 'POST',
        body: { totpCode },
      }),
  });
}

/** useStepUp confirms it's you: a TOTP code, or the password for a user without 2FA. */
export function useStepUp() {
  const setSession = useSessionUpdater();
  return useMutation({
    mutationFn: (request: StepUpRequest) =>
      apiRequest<SessionResponse>('api/auth/step-up', { method: 'POST', body: request }),
    onSuccess: (session) => {
      setSession(session);
    },
  });
}

/** useRemoveTOTP removes the signed-in user's authenticator app. */
export function useRemoveTOTP() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => apiRequest<undefined>('api/auth/totp', { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: sessionQueryKey }),
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => apiRequest<undefined>('api/auth/logout', { method: 'POST' }),
    onSettled: () => {
      // Signed out: forget everything that belonged to the user, but keep
      // the session and setup queries, which the page gates are watching.
      queryClient.setQueryData(sessionQueryKey, null);
      queryClient.removeQueries({
        predicate: (query) =>
          query.queryKey[0] !== sessionQueryKey[0] && query.queryKey[0] !== 'setup-status',
      });
    },
  });
}
