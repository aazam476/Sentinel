// SPDX-License-Identifier: Apache-2.0
import { toast } from 'sonner';

import { useEnrollmentTokens, useRevokeEnrollmentToken } from '../../api/enrollment';
import type { EnrollmentToken } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { FormError } from '../../components/FormError/FormError';
import { formatDateTime } from '../../formatting/formatting';
import styles from './AddMachinePage.module.css';

/** TokenList shows recent install commands and lets an admin cancel one. */
export function TokenList() {
  const tokens = useEnrollmentTokens();
  const revoke = useRevokeEnrollmentToken();
  const list = tokens.data?.tokens ?? [];

  const handleRevoke = (token: EnrollmentToken) => {
    revoke.mutate(token.id, {
      onSuccess: () => toast.success('The install command no longer works'),
    });
  };

  return (
    <section className={styles.section} aria-labelledby="tokens-title">
      <h2 id="tokens-title" className={styles.sectionTitle}>
        Recent install commands
      </h2>
      {list.length === 0 ? (
        <p className={styles.text}>{tokens.isPending ? 'Loading…' : 'None in the last week.'}</p>
      ) : (
        <ul className={styles.tokens}>
          {list.map((token) => (
            <li key={token.id} className={styles.token}>
              <div className={styles.tokenMain}>
                <span className={styles.tokenName}>
                  {token.note === '' ? `Command #${String(token.id)}` : token.note}
                </span>
                <span className={styles.tokenMeta}>
                  {describeToken(token)} · by {token.createdBy}
                </span>
              </div>
              {token.usable && (
                <Button
                  variant="secondary"
                  aria-label={`Cancel ${token.note === '' ? `command #${String(token.id)}` : token.note}`}
                  isBusy={revoke.isPending && revoke.variables === token.id}
                  onClick={() => {
                    handleRevoke(token);
                  }}
                >
                  Cancel
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <FormError error={tokens.error ?? revoke.error} />
    </section>
  );
}

function describeToken(token: EnrollmentToken): string {
  const used = `used ${String(token.useCount)} of ${String(token.maxUses)}`;
  if (token.revoked) {
    return `Cancelled · ${used}`;
  }
  if (!token.usable) {
    return token.useCount >= token.maxUses ? `Used up · ${used}` : `Expired · ${used}`;
  }
  return `Valid until ${formatDateTime(token.expiresAt)} · ${used}`;
}
