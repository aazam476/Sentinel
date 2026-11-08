// SPDX-License-Identifier: Apache-2.0
import { QueryClient } from '@tanstack/react-query';

/** newQueryClient sets how the app caches server data. */
export function newQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // A 4xx answer won't change by asking again; only retry once, and
        // only when the request itself failed.
        retry: (failureCount, error) => failureCount < 1 && error instanceof TypeError,
        refetchOnWindowFocus: false,
      },
    },
  });
}
