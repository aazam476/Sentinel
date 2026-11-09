// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { agentKeys } from './agents';
import { apiRequest } from './client';
import type {
  AgentBuild,
  AgentBuilds,
  FetchedBuilds,
  UpdateAgentsRequest,
  UpdateAgentsResponse,
} from './types.gen';

export const updateKeys = { builds: ['agent-builds'] as const };

/** useAgentBuilds lists the hosted agent builds and the update settings. */
export function useAgentBuilds() {
  return useQuery({
    queryKey: updateKeys.builds,
    queryFn: () => apiRequest<AgentBuilds>('api/agent-builds'),
  });
}

/** useUpdateChange runs a change, then refreshes builds and machines. */
function useUpdateChange<T, R>(change: (input: T) => Promise<R>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: change,
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: updateKeys.builds }),
        queryClient.invalidateQueries({ queryKey: agentKeys.all }),
      ]),
  });
}

/** useUploadBuild hosts a build and its release signature file. */
export function useUploadBuild() {
  return useUpdateChange(({ build, signature }: { build: File; signature: File }) => {
    const form = new FormData();
    // The signature first: the server checks the build as it arrives.
    form.append('signature', signature);
    form.append('build', build);
    return apiRequest<AgentBuild>('api/agent-builds', { method: 'POST', form });
  });
}

export function useFetchBuilds() {
  return useUpdateChange(() =>
    apiRequest<FetchedBuilds>('api/agent-builds/fetch', { method: 'POST' }),
  );
}

export function useSetAutoUpdate() {
  return useUpdateChange((enabled: boolean) =>
    apiRequest('api/agent-builds/auto-update', { method: 'PUT', body: { enabled } }),
  );
}

export function useUpdateAgents() {
  return useUpdateChange((request: UpdateAgentsRequest) =>
    apiRequest<UpdateAgentsResponse>('api/agent-updates', { method: 'POST', body: request }),
  );
}
