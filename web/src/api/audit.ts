// SPDX-License-Identifier: Apache-2.0
import { useInfiniteQuery, useMutation } from '@tanstack/react-query';

import { apiRequest, apiURL } from './client';
import type { AuditEntriesResponse, AuditVerifyResponse } from './types.gen';

const pageSize = 100;

/** useAuditLog loads the audit log page by page, newest first. */
export function useAuditLog() {
  return useInfiniteQuery({
    queryKey: ['audit-log'],
    initialPageParam: null as number | null,
    queryFn: ({ pageParam }) => {
      const before = pageParam === null ? '' : `&before=${String(pageParam)}`;
      return apiRequest<AuditEntriesResponse>(`api/audit?limit=${String(pageSize)}${before}`);
    },
    getNextPageParam: (lastPage) => lastPage.nextBefore,
  });
}

export function useVerifyAuditLog() {
  return useMutation({
    mutationFn: () => apiRequest<AuditVerifyResponse>('api/audit/verify'),
  });
}

/** auditExportURL is a download link for the whole log. */
export function auditExportURL(format: 'csv' | 'json'): string {
  return apiURL(`api/audit/export?format=${format}`);
}
