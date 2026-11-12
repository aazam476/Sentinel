// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest } from './client';
import type { SecuritySettings } from './types.gen';
import { userKeys } from './users';

export const securitySettingsQueryKey = ['security-settings'] as const;

/** useSecuritySettings reads the server-wide sign-in rules (ADR 0039). */
export function useSecuritySettings() {
  return useQuery({
    queryKey: securitySettingsQueryKey,
    queryFn: () => apiRequest<SecuritySettings>('api/settings/security'),
  });
}

/** useUpdateSecuritySettings changes them; admins only. */
export function useUpdateSecuritySettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (settings: SecuritySettings) =>
      apiRequest<SecuritySettings>('api/settings/security', { method: 'PUT', body: settings }),
    onSuccess: (settings) => {
      queryClient.setQueryData(securitySettingsQueryKey, settings);
      // Turning it on signs out users without 2FA, which the list shows.
      return queryClient.invalidateQueries({ queryKey: userKeys.all });
    },
  });
}
