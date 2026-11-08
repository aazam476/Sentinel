// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery } from '@tanstack/react-query';

import { apiRequest } from './client';
import type {
  BeginSetupRequest,
  BeginSetupResponse,
  CompleteSetupRequest,
  SetupStatusResponse,
  SignedInResponse,
} from './types.gen';

export const setupStatusQueryKey = ['setup-status'] as const;

export function useSetupStatus() {
  return useQuery({
    queryKey: setupStatusQueryKey,
    queryFn: () => apiRequest<SetupStatusResponse>('api/setup/status'),
  });
}

export function useBeginSetup() {
  return useMutation({
    mutationFn: (request: BeginSetupRequest) =>
      apiRequest<BeginSetupResponse>('api/setup/begin', { method: 'POST', body: request }),
  });
}

export function useCompleteSetup() {
  return useMutation({
    mutationFn: (request: CompleteSetupRequest) =>
      apiRequest<SignedInResponse>('api/setup/complete', { method: 'POST', body: request }),
  });
}
