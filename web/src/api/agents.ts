// SPDX-License-Identifier: Apache-2.0
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest } from './client';
import type { AgentDetails, AgentsResponse, MetricSeries, UpdateAgentRequest } from './types.gen';

/** Query keys for machines; the live event stream updates the same keys. */
export const agentKeys = {
  all: ['agents'] as const,
  list: () => [...agentKeys.all, 'list'] as const,
  detail: (id: string) => [...agentKeys.all, 'detail', id] as const,
  metrics: (id: string, metric: string, from: number, to: number) =>
    [...agentKeys.all, 'metrics', id, metric, from, to] as const,
};

/** useAgents lists every machine that hasn't been revoked. */
export function useAgents() {
  return useQuery({
    queryKey: agentKeys.list(),
    queryFn: () => apiRequest<AgentsResponse>('api/agents'),
  });
}

/** useAgent is one machine with its details. */
export function useAgent(id: string) {
  return useQuery({
    queryKey: agentKeys.detail(id),
    queryFn: () => apiRequest<AgentDetails>(`api/agents/${encodeURIComponent(id)}`),
  });
}

/**
 * useAgentMetrics is one metric of a machine over [from, to) in Unix
 * milliseconds. While a new range loads, the previous series stays on
 * screen so the chart doesn't flash empty.
 */
export function useAgentMetrics(id: string, metric: string, from: number, to: number) {
  const query = new URLSearchParams({ metric, from: String(from), to: String(to) });
  return useQuery({
    queryKey: agentKeys.metrics(id, metric, from, to),
    queryFn: () =>
      apiRequest<MetricSeries>(`api/agents/${encodeURIComponent(id)}/metrics?${query.toString()}`),
    placeholderData: keepPreviousData,
  });
}

/** useUpdateAgent renames or re-tags a machine. */
export function useUpdateAgent(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (request: UpdateAgentRequest) =>
      apiRequest<AgentDetails>(`api/agents/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body: request,
      }),
    onSuccess: (updated) => {
      queryClient.setQueryData(agentKeys.detail(id), updated);
      return queryClient.invalidateQueries({ queryKey: agentKeys.list() });
    },
  });
}

/** useRevokeAgent revokes a machine; it disconnects at once. */
export function useRevokeAgent(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () =>
      apiRequest<undefined>(`api/agents/${encodeURIComponent(id)}/revoke`, { method: 'POST' }),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: agentKeys.detail(id) });
      return queryClient.invalidateQueries({ queryKey: agentKeys.list() });
    },
  });
}
