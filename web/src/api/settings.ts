// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest } from './client';
import type { SettingsRequest, SettingsResponse } from './types.gen';

const settingsQueryKey = ['settings'] as const;

/** useSettings reads the Settings page (§9.10); admins only. */
export function useSettings() {
  return useQuery({
    queryKey: settingsQueryKey,
    queryFn: () => apiRequest<SettingsResponse>('api/settings'),
  });
}

/** useSaveSettings changes the settings admins change here. */
export function useSaveSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (request: SettingsRequest) =>
      apiRequest<SettingsResponse>('api/settings', { method: 'PUT', body: request }),
    onSuccess: (saved) => {
      queryClient.setQueryData(settingsQueryKey, saved);
    },
  });
}
