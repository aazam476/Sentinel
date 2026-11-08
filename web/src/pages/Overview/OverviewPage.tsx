// SPDX-License-Identifier: Apache-2.0
import { useOutletContext } from 'react-router';

import type { SessionResponse } from '../../api/types.gen';
import styles from './OverviewPage.module.css';

/**
 * OverviewPage is the signed-in home page. The fleet view arrives with the
 * agent; until then it says so.
 */
export function OverviewPage() {
  const session = useOutletContext<SessionResponse>();

  return (
    <div className={styles.page}>
      <h1 className={styles.title}>Welcome, {session.user.username}</h1>
      <section className={styles.empty} aria-labelledby="no-machines">
        <h2 id="no-machines" className={styles.emptyTitle}>
          No machines yet
        </h2>
        <p className={styles.emptyText}>
          Managed machines will appear here once agents can be added to Sentinel.
        </p>
      </section>
    </div>
  );
}
