// SPDX-License-Identifier: Apache-2.0
import { toast } from 'sonner';

import { useRevokeSession, useSessions } from '../../api/sessions';
import type { SessionSummary } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { FormError } from '../../components/FormError/FormError';
import { describeUserAgent, formatDateTime } from '../../formatting/formatting';
import styles from './AccountPage.module.css';

/** SessionsSection lists where the user is signed in and signs others out (SEC-06). */
export function SessionsSection() {
  const sessions = useSessions();
  const revoke = useRevokeSession();

  const handleRevoke = (session: SessionSummary) => {
    revoke.mutate(session.id, {
      onSuccess: () => toast.success(`Signed out ${describeUserAgent(session.userAgent)}`),
    });
  };

  return (
    <section className={styles.section} aria-labelledby="sessions-title">
      <h2 id="sessions-title" className={styles.sectionTitle}>
        Signed-in sessions
      </h2>
      <p className={styles.sectionText}>Sign out any session you don’t recognize.</p>
      {sessions.isPending ? (
        <p className={styles.sectionText}>Loading…</p>
      ) : (
        <ul className={styles.list}>
          {sessions.data?.sessions.map((session) => (
            <li key={session.id} className={styles.item}>
              <div className={styles.itemMain}>
                <span className={styles.itemName}>
                  {describeUserAgent(session.userAgent)}
                  {session.current && <span className={styles.badge}>This browser</span>}
                </span>
                <span className={styles.itemMeta}>
                  {session.ip} · Last active {formatDateTime(session.lastSeenAt)}
                </span>
              </div>
              {!session.current && (
                <Button
                  variant="secondary"
                  aria-label={`Sign out ${describeUserAgent(session.userAgent)} from ${session.ip}`}
                  isBusy={revoke.isPending && revoke.variables === session.id}
                  onClick={() => {
                    handleRevoke(session);
                  }}
                >
                  Sign out
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <FormError error={sessions.error ?? revoke.error} />
    </section>
  );
}
