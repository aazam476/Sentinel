// SPDX-License-Identifier: Apache-2.0
import { useQuery } from '@tanstack/react-query';

import { apiRequest } from './client';
import type { AboutResponse } from './types.gen';

/** useAbout describes the server and its fleet (§9.11). */
export function useAbout() {
  return useQuery({
    queryKey: ['about'],
    queryFn: () => apiRequest<AboutResponse>('api/about'),
  });
}
