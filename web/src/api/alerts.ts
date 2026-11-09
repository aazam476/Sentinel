// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { agentKeys } from './agents';
import { apiRequest } from './client';
import type {
  AgentDetails,
  AlertRule,
  AlertRules,
  FleetAlerts,
  NotificationChannel,
  NotificationChannels,
  SaveAlertRuleRequest,
  SaveNotificationChannelRequest,
} from './types.gen';

/**
 * Query keys for alerts. Every alert list sits under agentKeys.all's
 * "alerts", so an alerts event from the server refreshes them all.
 */
export const alertKeys = {
  all: [...agentKeys.all, 'alerts'] as const,
  fleet: () => [...alertKeys.all, 'fleet'] as const,
  rules: ['alert-rules'] as const,
  channels: ['notification-channels'] as const,
};

/** useAlerts lists every firing alert, then recently resolved ones. */
export function useAlerts() {
  return useQuery({
    queryKey: alertKeys.fleet(),
    queryFn: () => apiRequest<FleetAlerts>('api/alerts'),
  });
}

/** useAlertChange runs a change, then refreshes alerts, rules and channels. */
function useAlertChange<T, R>(change: (input: T) => Promise<R>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: change,
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: alertKeys.all }),
        queryClient.invalidateQueries({ queryKey: alertKeys.rules }),
        queryClient.invalidateQueries({ queryKey: alertKeys.channels }),
      ]),
  });
}

export function useAcknowledgeAlert() {
  return useAlertChange((id: number) =>
    apiRequest(`api/alerts/${String(id)}/acknowledge`, { method: 'POST' }),
  );
}

export function useAlertRules() {
  return useQuery({
    queryKey: alertKeys.rules,
    queryFn: () => apiRequest<AlertRules>('api/alert-rules'),
  });
}

export function useSaveAlertRule() {
  return useAlertChange(({ id, ...request }: SaveAlertRuleRequest & { id?: number }) =>
    apiRequest<AlertRule>(id === undefined ? 'api/alert-rules' : `api/alert-rules/${String(id)}`, {
      method: id === undefined ? 'POST' : 'PUT',
      body: request,
    }),
  );
}

export function useDeleteAlertRule() {
  return useAlertChange((id: number) =>
    apiRequest(`api/alert-rules/${String(id)}`, { method: 'DELETE' }),
  );
}

export function useChannels() {
  return useQuery({
    queryKey: alertKeys.channels,
    queryFn: () => apiRequest<NotificationChannels>('api/notification-channels'),
  });
}

export function useSaveChannel() {
  return useAlertChange(({ id, ...request }: SaveNotificationChannelRequest & { id?: number }) =>
    apiRequest<NotificationChannel>(
      id === undefined ? 'api/notification-channels' : `api/notification-channels/${String(id)}`,
      { method: id === undefined ? 'POST' : 'PUT', body: request },
    ),
  );
}

export function useDeleteChannel() {
  return useAlertChange((id: number) =>
    apiRequest(`api/notification-channels/${String(id)}`, { method: 'DELETE' }),
  );
}

/** useTestChannel sends a test notification through a channel. */
export function useTestChannel() {
  return useAlertChange((id: number) =>
    apiRequest(`api/notification-channels/${String(id)}/test`, { method: 'POST' }),
  );
}

/**
 * useSetMaintenance puts a machine in maintenance until a time (Unix
 * milliseconds), or takes it out with null.
 */
export function useSetMaintenance(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (until: number | null) =>
      apiRequest<AgentDetails>(`api/agents/${encodeURIComponent(id)}/maintenance`, {
        method: 'PUT',
        body: { until },
      }),
    onSuccess: (updated) => {
      queryClient.setQueryData(agentKeys.detail(id), updated);
      return queryClient.invalidateQueries({ queryKey: agentKeys.list() });
    },
  });
}
