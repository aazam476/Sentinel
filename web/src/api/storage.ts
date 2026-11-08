// SPDX-License-Identifier: Apache-2.0
import { useQuery } from '@tanstack/react-query';

import { agentKeys } from './agents';
import { apiRequest } from './client';
import type { StorageOverviewPayload } from './protocol.gen';
import type { MachineAlerts } from './types.gen';

const agent = (id: string) => `api/agents/${encodeURIComponent(id)}`;

export const storageKeys = {
  overview: (id: string) => [...agentKeys.all, 'storage', id] as const,
  alerts: (id: string) => [...agentKeys.all, 'alerts', id] as const,
};

/**
 * useStorage reads a machine's storage when the tab opens and every 60
 * seconds while it stays open (§9.5).
 */
export function useStorage(id: string, enabled: boolean) {
  return useQuery({
    queryKey: storageKeys.overview(id),
    queryFn: () => apiRequest<StorageOverviewPayload>(`${agent(id)}/storage`),
    refetchInterval: 60_000,
    // Always read fresh when the tab opens, not a minute-old copy.
    refetchOnMount: 'always',
    enabled,
    retry: false,
  });
}

/** useMachineAlerts lists a machine's firing and recently resolved alerts. */
export function useMachineAlerts(id: string) {
  return useQuery({
    queryKey: storageKeys.alerts(id),
    queryFn: () => apiRequest<MachineAlerts>(`${agent(id)}/alerts`),
    refetchInterval: 60_000,
  });
}
