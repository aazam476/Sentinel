// SPDX-License-Identifier: Apache-2.0
import { useOutletContext } from 'react-router';

import { auditExportURL, useAuditLog } from '../../api/audit';
import type { SessionResponse } from '../../api/types.gen';
import { FormError } from '../../components/FormError/FormError';
import { AuditList } from './AuditList';
import styles from './AuditPage.module.css';
import { VerifyPanel } from './VerifyPanel';

/** AuditPage shows, verifies and exports the audit log, for admins (SEC-41). */
export function AuditPage() {
  const session = useOutletContext<SessionResponse>();
  if (session.user.role !== 'admin') {
    return (
      <div className={styles.page}>
        <h1 className={styles.title}>Audit log</h1>
        <p className={styles.text}>Only admins can see the audit log.</p>
      </div>
    );
  }
  return <AdminAuditPage />;
}

function AdminAuditPage() {
  const auditLog = useAuditLog();
  const entries = auditLog.data?.pages.flatMap((page) => page.entries) ?? [];

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <h1 className={styles.title}>Audit log</h1>
        <div className={styles.exports}>
          <a className={styles.exportLink} href={auditExportURL('csv')} download>
            Export CSV
          </a>
          <a className={styles.exportLink} href={auditExportURL('json')} download>
            Export JSON
          </a>
        </div>
      </div>
      <p className={styles.text}>
        Every sign-in, change and refusal, newest first. Entries can’t be edited or removed.
      </p>
      <VerifyPanel />
      <FormError error={auditLog.error} />
      {auditLog.isPending ? (
        <p className={styles.text}>Loading…</p>
      ) : (
        <AuditList
          entries={entries}
          onEndReached={() => {
            if (auditLog.hasNextPage && !auditLog.isFetchingNextPage) {
              void auditLog.fetchNextPage();
            }
          }}
        />
      )}
    </div>
  );
}
