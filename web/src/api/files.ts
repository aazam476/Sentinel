// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { agentKeys } from './agents';
import { apiRequest, apiURL, isApiError } from './client';
import type {
  FileEntry,
  FileListResultPayload,
  FileTextPayload,
  FileUploadPayload,
} from './protocol.gen';
import type { FileUploadRequest } from './types.gen';

const files = (id: string) => `api/agents/${encodeURIComponent(id)}/files`;
const withPath = (path: string) => `?${new URLSearchParams({ path }).toString()}`;

export const fileKeys = {
  all: (id: string) => [...agentKeys.all, 'files', id] as const,
  list: (id: string, path: string) => [...fileKeys.all(id), 'list', path] as const,
  text: (id: string, path: string) => [...fileKeys.all(id), 'text', path] as const,
};

/** useFileList lists a folder; an empty path lists the starting points. */
export function useFileList(id: string, path: string) {
  return useQuery({
    queryKey: fileKeys.list(id, path),
    queryFn: () => apiRequest<FileListResultPayload>(`${files(id)}${withPath(path)}`),
    retry: false,
  });
}

/** downloadURL is where the browser downloads a file from. */
export function downloadURL(id: string, path: string): string {
  return apiURL(`${files(id)}/download${withPath(path)}`);
}

/** useFileChange runs a change, then refreshes the machine's listings. */
function useFileChange<T, R>(id: string, change: (input: T) => Promise<R>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: change,
    onSettled: () => queryClient.invalidateQueries({ queryKey: fileKeys.all(id) }),
  });
}

export function useMkdir(id: string) {
  return useFileChange(id, (path: string) =>
    apiRequest(`${files(id)}/mkdir`, { method: 'POST', body: { path } }),
  );
}

export function useRename(id: string) {
  return useFileChange(id, (request: { from: string; to: string }) =>
    apiRequest(`${files(id)}/rename`, { method: 'POST', body: request }),
  );
}

export function useDeleteFile(id: string) {
  return useFileChange(id, ({ path, recursive }: { path: string; recursive: boolean }) =>
    apiRequest(
      `${files(id)}?${new URLSearchParams({ path, recursive: String(recursive) }).toString()}`,
      { method: 'DELETE' },
    ),
  );
}

export function useTextFile(id: string, path: string) {
  return useQuery({
    queryKey: fileKeys.text(id, path),
    queryFn: () => apiRequest<FileTextPayload>(`${files(id)}/text${withPath(path)}`),
    retry: false,
    // An open editor shouldn't change under the person typing in it.
    refetchOnWindowFocus: false,
  });
}

export function useSaveText(id: string) {
  return useFileChange(id, (request: FileTextPayload) =>
    apiRequest<FileEntry>(`${files(id)}/text`, { method: 'PUT', body: request }),
  );
}

/** uploadPiece is how much of a file one request carries (the server's limit). */
export const uploadPiece = 8 * 1024 * 1024;

/** retries is how many times an upload resumes after a failed piece. */
const retries = 3;

export interface UploadOptions {
  overwrite: boolean;
  onProgress: (sentBytes: number) => void;
  signal: AbortSignal;
}

/**
 * uploadFile sends a file in pieces. After a failed piece it begins the
 * upload again, which tells it how much the machine has, and carries on
 * from there; a file is never sent twice from the start.
 */
export async function uploadFile(
  id: string,
  path: string,
  file: File,
  { overwrite, onProgress, signal }: UploadOptions,
): Promise<FileEntry> {
  const begin = () =>
    apiRequest<FileUploadPayload>(`${files(id)}/uploads`, {
      method: 'POST',
      body: { path, sizeBytes: file.size, overwrite } satisfies FileUploadRequest,
      signal,
    });
  let state = await begin();
  let failures = 0;
  while (state.receivedBytes < file.size) {
    onProgress(state.receivedBytes);
    const piece = file.slice(state.receivedBytes, state.receivedBytes + uploadPiece);
    try {
      state = await apiRequest<FileUploadPayload>(
        `${files(id)}/uploads/${encodeURIComponent(state.uploadId)}?offset=${String(state.receivedBytes)}`,
        { method: 'PUT', raw: piece, signal },
      );
      failures = 0;
    } catch (error) {
      failures += 1;
      if (
        signal.aborted ||
        failures > retries ||
        (isApiError(error) &&
          error.status < 500 &&
          error.code !== 'conflict' &&
          error.code !== 'not_found')
      ) {
        throw error;
      }
      state = await begin();
    }
  }
  onProgress(file.size);
  return apiRequest<FileEntry>(
    `${files(id)}/uploads/${encodeURIComponent(state.uploadId)}/finish`,
    { method: 'POST', signal },
  );
}

/** cancelUpload stops an upload and deletes what arrived of it. */
export async function cancelUpload(id: string, path: string, size: number): Promise<void> {
  const state = await apiRequest<FileUploadPayload>(`${files(id)}/uploads`, {
    method: 'POST',
    body: { path, sizeBytes: size, overwrite: true } satisfies FileUploadRequest,
  });
  await apiRequest(`${files(id)}/uploads/${encodeURIComponent(state.uploadId)}`, {
    method: 'DELETE',
  });
}
