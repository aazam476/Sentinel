// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest } from './client';
import type {
  CreateEnrollmentTokenRequest,
  CreateEnrollmentTokenResponse,
  EnrollmentTokensResponse,
} from './types.gen';

const tokensQueryKey = ['enrollment-tokens'] as const;

/** useEnrollmentTokens lists recent enrollment tokens (admins only). */
export function useEnrollmentTokens() {
  return useQuery({
    queryKey: tokensQueryKey,
    queryFn: () => apiRequest<EnrollmentTokensResponse>('api/enrollment-tokens'),
  });
}

/** useCreateEnrollmentToken makes a token; its secret is in the response only. */
export function useCreateEnrollmentToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (request: CreateEnrollmentTokenRequest) =>
      apiRequest<CreateEnrollmentTokenResponse>('api/enrollment-tokens', {
        method: 'POST',
        body: request,
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

/** useRevokeEnrollmentToken stops a token from enrolling more machines. */
export function useRevokeEnrollmentToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (tokenId: number) =>
      apiRequest<undefined>(`api/enrollment-tokens/${String(tokenId)}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}
