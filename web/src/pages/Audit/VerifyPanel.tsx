// SPDX-License-Identifier: Apache-2.0
import { useVerifyAuditLog } from '../../api/audit';
import { Button } from '../../components/Button/Button';
import { FormError } from '../../components/FormError/FormError';
import { countOf } from '../../formatting/formatting';
import styles from './AuditPage.module.css';

/** VerifyPanel checks the audit log's hash chain on demand (SEC-41). */
export function VerifyPanel() {
  const verify = useVerifyAuditLog();
  const result = verify.data;

  return (
    <div className={styles.verify}>
      <Button
        variant="secondary"
        isBusy={verify.isPending}
        onClick={() => {
          verify.mutate();
        }}
      >
        Verify integrity
      </Button>
      <div role="status" className={styles.verifyResult}>
        {result?.valid === true && (
          <span className={styles.valid}>
            Intact: {countOf(result.checked, 'entry checks', 'entries check')} out.
          </span>
        )}
        {result?.valid === false && (
          <span className={styles.invalid}>
            Tampering detected at entry {result.brokenAtId}: {result.problem}
          </span>
        )}
      </div>
      <FormError error={verify.error} />
    </div>
  );
}
