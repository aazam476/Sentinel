// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { agentKeys } from './agents';
import { apiRequest } from './client';
import type {
  ApplyPackagesRequest,
  ApplyPackagesResponse,
  PackageUpdatesResponse,
} from './types.gen';

export const packageKeys = {
  updates: (id: string) => [...agentKeys.all, 'packages', id] as const,
};

const packagesPath = (id: string) => `api/agents/${encodeURIComponent(id)}/packages`;

/**
 * usePackageUpdates lists a machine's waiting updates (§9.6). Each check
 * refreshes the machine's package catalogs, so it is kept for ten minutes
 * rather than asked again on every visit.
 */
export function usePackageUpdates(id: string) {
  return useQuery({
    queryKey: packageKeys.updates(id),
    queryFn: () => apiRequest<PackageUpdatesResponse>(packagesPath(id)),
    staleTime: 10 * 60 * 1000,
    retry: false,
  });
}

/** useInstallUpdates installs some or all of one manager's updates. */
export function useInstallUpdates(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (request: ApplyPackagesRequest) =>
      apiRequest<ApplyPackagesResponse>(packagesPath(id), { method: 'POST', body: request }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: packageKeys.updates(id) }),
  });
}
