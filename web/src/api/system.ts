// SPDX-License-Identifier: Apache-2.0
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { agentKeys } from './agents';
import { apiRequest } from './client';
import type {
  KillProcessRequest,
  LogEntries,
  MachineLogEntry,
  PowerRequest,
  ProcessList,
  ServiceList,
  WakeResponse,
} from './types.gen';

export type LogLevel = MachineLogEntry['level'];
export type ServiceAction = 'start' | 'stop' | 'restart' | 'enable' | 'disable';
export type PowerAction = PowerRequest['action'];

/** LogFilter is what the log view asks for. */
export interface LogFilter {
  service?: string;
  minLevel?: LogLevel;
  /** sinceMs is how far back to look, in milliseconds. */
  sinceMs: number;
}

export const systemKeys = {
  services: (id: string) => [...agentKeys.all, 'services', id] as const,
  processes: (id: string) => [...agentKeys.all, 'processes', id] as const,
  logs: (id: string, filter: LogFilter) => [...agentKeys.all, 'logs', id, filter] as const,
};

const machinePath = (id: string) => `api/agents/${encodeURIComponent(id)}`;

/** useServices lists a machine's services. */
export function useServices(id: string) {
  return useQuery({
    queryKey: systemKeys.services(id),
    queryFn: () => apiRequest<ServiceList>(`${machinePath(id)}/services`),
    retry: false,
  });
}

/** useServiceAction starts, stops, restarts, enables or disables a service. */
export function useServiceAction(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ name, action }: { name: string; action: ServiceAction }) =>
      apiRequest<undefined>(`${machinePath(id)}/services/${encodeURIComponent(name)}/${action}`, {
        method: 'POST',
      }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: systemKeys.services(id) }),
  });
}

/**
 * useProcesses lists a machine's processes, refreshing every 5 seconds
 * while the page is visible. Each list takes the agent about a second, to
 * measure CPU use.
 */
export function useProcesses(id: string, isPaused: boolean) {
  return useQuery({
    queryKey: systemKeys.processes(id),
    queryFn: () => apiRequest<ProcessList>(`${machinePath(id)}/processes`),
    refetchInterval: isPaused ? false : 5000,
    placeholderData: keepPreviousData,
    retry: false,
  });
}

/** useKillProcess ends a process. */
export function useKillProcess(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ pid, force }: { pid: number } & KillProcessRequest) =>
      apiRequest<undefined>(`${machinePath(id)}/processes/${String(pid)}/kill`, {
        method: 'POST',
        body: { force } satisfies KillProcessRequest,
      }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: systemKeys.processes(id) }),
  });
}

/**
 * useLogs reads a machine's log, newest first. The start time is fixed when
 * the filter changes, so refetching shows what is new.
 */
export function useLogs(id: string, filter: LogFilter, now: number) {
  const query = new URLSearchParams({ since: String(now - filter.sinceMs), limit: '1000' });
  if (filter.service !== undefined && filter.service !== '') {
    query.set('service', filter.service);
  }
  if (filter.minLevel !== undefined) {
    query.set('level', filter.minLevel);
  }
  return useQuery({
    queryKey: systemKeys.logs(id, filter),
    queryFn: () => apiRequest<LogEntries>(`${machinePath(id)}/logs?${query.toString()}`),
    placeholderData: keepPreviousData,
    retry: false,
  });
}

/** useWake wakes an offline machine through a neighbour on its network. */
export function useWake(id: string) {
  return useMutation({
    mutationFn: () => apiRequest<WakeResponse>(`${machinePath(id)}/wake`, { method: 'POST' }),
  });
}

/** usePower reboots or shuts down a machine. */
export function usePower(id: string) {
  return useMutation({
    mutationFn: (action: PowerAction) =>
      apiRequest<undefined>(`${machinePath(id)}/power`, {
        method: 'POST',
        body: { action } satisfies PowerRequest,
      }),
  });
}
