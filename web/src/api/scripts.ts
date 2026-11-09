// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest } from './client';
import type {
  RunScriptRequest,
  RunScriptResponse,
  SaveScheduleRequest,
  SaveScriptRequest,
  Schedule,
  Schedules,
  Script,
  ScriptBatchDetails,
  ScriptBatches,
  ScriptRunOutput,
  Scripts,
} from './types.gen';

export const scriptKeys = {
  all: ['scripts'] as const,
  library: ['scripts', 'library'] as const,
  batches: ['scripts', 'batches'] as const,
  batch: (id: number) => ['scripts', 'batches', id] as const,
  run: (id: number) => ['scripts', 'runs', id] as const,
  schedules: ['scripts', 'schedules'] as const,
};

/** isRunning says whether a run hasn't ended yet. */
export function isRunning(state: string): boolean {
  return state === 'running';
}

export function useScripts() {
  return useQuery({
    queryKey: scriptKeys.library,
    queryFn: () => apiRequest<Scripts>('api/scripts'),
  });
}

/** useScriptChange runs a change, then refreshes everything about scripts. */
function useScriptChange<T, R>(change: (input: T) => Promise<R>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: change,
    onSettled: () => queryClient.invalidateQueries({ queryKey: scriptKeys.all }),
  });
}

export function useSaveScript() {
  return useScriptChange(({ id, ...request }: SaveScriptRequest & { id?: number }) =>
    apiRequest<Script>(id === undefined ? 'api/scripts' : `api/scripts/${String(id)}`, {
      method: id === undefined ? 'POST' : 'PUT',
      body: request,
    }),
  );
}

export function useDeleteScript() {
  return useScriptChange((id: number) =>
    apiRequest(`api/scripts/${String(id)}`, { method: 'DELETE' }),
  );
}

export function useRunScript() {
  return useScriptChange(({ id, ...request }: RunScriptRequest & { id: number }) =>
    apiRequest<RunScriptResponse>(`api/scripts/${String(id)}/run`, {
      method: 'POST',
      body: request,
    }),
  );
}

/** useBatches lists recent runs, refreshing while any is going. */
export function useBatches() {
  return useQuery({
    queryKey: scriptKeys.batches,
    queryFn: () => apiRequest<ScriptBatches>('api/script-batches'),
    refetchInterval: (query) =>
      (query.state.data?.batches ?? []).some((batch) => (batch.counts.running ?? 0) > 0)
        ? 2_000
        : 30_000,
  });
}

/** useBatch reads one batch, every second while any of its runs is going. */
export function useBatch(id: number) {
  return useQuery({
    queryKey: scriptKeys.batch(id),
    queryFn: () => apiRequest<ScriptBatchDetails>(`api/script-batches/${String(id)}`),
    refetchInterval: (query) =>
      (query.state.data?.runs ?? []).some((run) => isRunning(run.state)) ? 1_000 : false,
    retry: false,
  });
}

/** useRunOutput follows a run's output, every second while it runs. */
export function useRunOutput(id: number) {
  return useQuery({
    queryKey: scriptKeys.run(id),
    queryFn: () => apiRequest<ScriptRunOutput>(`api/script-runs/${String(id)}`),
    refetchInterval: (query) =>
      query.state.data === undefined || isRunning(query.state.data.state) ? 1_000 : false,
  });
}

export function useCancelRun() {
  return useScriptChange((id: number) =>
    apiRequest(`api/script-runs/${String(id)}/cancel`, { method: 'POST' }),
  );
}

export function useSchedules() {
  return useQuery({
    queryKey: scriptKeys.schedules,
    queryFn: () => apiRequest<Schedules>('api/schedules'),
  });
}

export function useSaveSchedule() {
  return useScriptChange(({ id, ...request }: SaveScheduleRequest & { id?: number }) =>
    apiRequest<Schedule>(id === undefined ? 'api/schedules' : `api/schedules/${String(id)}`, {
      method: id === undefined ? 'POST' : 'PUT',
      body: request,
    }),
  );
}

export function useDeleteSchedule() {
  return useScriptChange((id: number) =>
    apiRequest(`api/schedules/${String(id)}`, { method: 'DELETE' }),
  );
}
