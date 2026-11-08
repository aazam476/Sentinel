// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { agentKeys } from './agents';
import { apiRequest } from './client';
import type {
  ContainerInspectPayload,
  ContainerListPayload,
  ContainerLogsResultPayload,
  NetworkListPayload,
  StackFilesPayload,
  StackListPayload,
  VolumeListPayload,
} from './protocol.gen';
import type {
  AddRegistryLoginRequest,
  DockerImages,
  ImageReferenceRequest,
  RegistryLogin,
  RegistryLogins,
  SaveStackRequest,
} from './types.gen';

export type ContainerAction = 'start' | 'stop' | 'restart';
export type StackAction = 'up' | 'down' | 'pull';

const docker = (id: string) => `api/agents/${encodeURIComponent(id)}/docker`;

export const dockerKeys = {
  all: (id: string) => [...agentKeys.all, 'docker', id] as const,
  containers: (id: string) => [...dockerKeys.all(id), 'containers'] as const,
  images: (id: string) => [...dockerKeys.all(id), 'images'] as const,
  volumes: (id: string) => [...dockerKeys.all(id), 'volumes'] as const,
  networks: (id: string) => [...dockerKeys.all(id), 'networks'] as const,
  stacks: (id: string) => [...dockerKeys.all(id), 'stacks'] as const,
  stack: (id: string, name: string) => [...dockerKeys.all(id), 'stack', name] as const,
  logs: (id: string, container: string) => [...dockerKeys.all(id), 'logs', container] as const,
  inspect: (id: string, container: string) =>
    [...dockerKeys.all(id), 'inspect', container] as const,
  registries: ['registries'] as const,
};

/** useContainers lists a machine's containers, refreshing every 10 seconds. */
export function useContainers(id: string) {
  return useQuery({
    queryKey: dockerKeys.containers(id),
    queryFn: () => apiRequest<ContainerListPayload>(`${docker(id)}/containers`),
    refetchInterval: 10_000,
    retry: false,
  });
}

/**
 * useDockerChange runs a Docker change and refreshes everything Docker on
 * the machine afterwards: one change (a stack going down, a pull) often
 * affects several lists.
 */
function useDockerChange<T>(id: string, change: (input: T) => Promise<unknown>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: change,
    onSettled: () => queryClient.invalidateQueries({ queryKey: dockerKeys.all(id) }),
  });
}

export function useContainerAction(id: string) {
  return useDockerChange(id, ({ name, action }: { name: string; action: ContainerAction }) =>
    apiRequest(`${docker(id)}/containers/${encodeURIComponent(name)}/${action}`, {
      method: 'POST',
    }),
  );
}

export function useRemoveContainer(id: string) {
  return useDockerChange(id, ({ name, force }: { name: string; force: boolean }) =>
    apiRequest(`${docker(id)}/containers/${encodeURIComponent(name)}?force=${String(force)}`, {
      method: 'DELETE',
    }),
  );
}

export function useInspect(id: string, container: string, enabled: boolean) {
  return useQuery({
    queryKey: dockerKeys.inspect(id, container),
    queryFn: () =>
      apiRequest<ContainerInspectPayload>(
        `${docker(id)}/containers/${encodeURIComponent(container)}/inspect`,
      ),
    enabled,
    retry: false,
  });
}

export function useContainerLogs(id: string, container: string) {
  return useQuery({
    queryKey: dockerKeys.logs(id, container),
    queryFn: () =>
      apiRequest<ContainerLogsResultPayload>(
        `${docker(id)}/containers/${encodeURIComponent(container)}/logs?tail=1000`,
      ),
    retry: false,
  });
}

/** followLogsURL is the server-sent event stream of a container's log. */
export function followLogsPath(id: string, container: string): string {
  return `${docker(id)}/containers/${encodeURIComponent(container)}/logs/follow?tail=1000`;
}

export function useImages(id: string) {
  return useQuery({
    queryKey: dockerKeys.images(id),
    queryFn: () => apiRequest<DockerImages>(`${docker(id)}/images`),
    retry: false,
  });
}

export function usePullImage(id: string) {
  return useDockerChange(id, (reference: string) =>
    apiRequest(`${docker(id)}/images/pull`, {
      method: 'POST',
      body: { reference } satisfies ImageReferenceRequest,
    }),
  );
}

export function useCheckImages(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => apiRequest<DockerImages>(`${docker(id)}/images/check`, { method: 'POST' }),
    onSuccess: (images) => {
      queryClient.setQueryData(dockerKeys.images(id), images);
    },
  });
}

export function useRemoveImage(id: string) {
  return useDockerChange(id, ({ reference, force }: { reference: string; force: boolean }) =>
    apiRequest(
      `${docker(id)}/images?${new URLSearchParams({ reference, force: String(force) }).toString()}`,
      { method: 'DELETE' },
    ),
  );
}

export function useVolumes(id: string) {
  return useQuery({
    queryKey: dockerKeys.volumes(id),
    queryFn: () => apiRequest<VolumeListPayload>(`${docker(id)}/volumes`),
    retry: false,
  });
}

export function useNetworks(id: string) {
  return useQuery({
    queryKey: dockerKeys.networks(id),
    queryFn: () => apiRequest<NetworkListPayload>(`${docker(id)}/networks`),
    retry: false,
  });
}

export function useRemoveVolume(id: string) {
  return useDockerChange(id, (name: string) =>
    apiRequest(`${docker(id)}/volumes/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  );
}

export function useRemoveNetwork(id: string) {
  return useDockerChange(id, (name: string) =>
    apiRequest(`${docker(id)}/networks/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  );
}

export function useStacks(id: string) {
  return useQuery({
    queryKey: dockerKeys.stacks(id),
    queryFn: () => apiRequest<StackListPayload>(`${docker(id)}/stacks`),
    retry: false,
  });
}

export function useStack(id: string, name: string, enabled: boolean) {
  return useQuery({
    queryKey: dockerKeys.stack(id, name),
    queryFn: () =>
      apiRequest<StackFilesPayload>(`${docker(id)}/stacks/${encodeURIComponent(name)}`),
    enabled,
    retry: false,
  });
}

export function useSaveStack(id: string) {
  return useDockerChange(id, ({ name, ...request }: SaveStackRequest & { name: string }) =>
    apiRequest(`${docker(id)}/stacks/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: request,
    }),
  );
}

export function useStackAction(id: string) {
  return useDockerChange(id, ({ name, action }: { name: string; action: StackAction }) =>
    apiRequest(`${docker(id)}/stacks/${encodeURIComponent(name)}/${action}`, { method: 'POST' }),
  );
}

export function useRegistryLogins(enabled: boolean) {
  return useQuery({
    queryKey: dockerKeys.registries,
    queryFn: () => apiRequest<RegistryLogins>('api/registries'),
    enabled,
  });
}

export function useAddRegistryLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (request: AddRegistryLoginRequest) =>
      apiRequest<RegistryLogin>('api/registries', { method: 'POST', body: request }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: dockerKeys.registries }),
  });
}

export function useRemoveRegistryLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (loginId: number) =>
      apiRequest(`api/registries/${String(loginId)}`, { method: 'DELETE' }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: dockerKeys.registries }),
  });
}
