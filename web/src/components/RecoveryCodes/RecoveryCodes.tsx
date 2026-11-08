// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { toast } from 'sonner';

import { Button } from '../Button/Button';
import styles from './RecoveryCodes.module.css';

interface RecoveryCodesProps {
  codes: string[];
  /** onDone runs once the person confirms they saved the codes. */
  onDone: () => void;
}

/**
 * RecoveryCodes shows the one-time recovery codes after 2FA setup. They are
 * never shown again, so continuing needs an explicit confirmation.
 */
export function RecoveryCodes({ codes, onDone }: RecoveryCodesProps) {
  const [hasSaved, setHasSaved] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(codes.join('\n')).then(
      () => toast.success('Recovery codes copied'),
      () => toast.error('Copying failed. Select the codes and copy them by hand.'),
    );
  };

  const downloadURL = `data:text/plain;charset=utf-8,${encodeURIComponent(
    `Sentinel recovery codes. Each works once.\n\n${codes.join('\n')}\n`,
  )}`;

  return (
    <section className={styles.panel} aria-labelledby="recovery-codes-title">
      <h2 id="recovery-codes-title" className={styles.title}>
        Save your recovery codes
      </h2>
      <p className={styles.text}>
        If you lose your authenticator, each of these codes signs you in once. Keep them somewhere
        safe. They won’t be shown again.
      </p>
      <ol className={styles.codes} aria-label="Recovery codes">
        {codes.map((code) => (
          <li key={code} className={styles.code}>
            {code}
          </li>
        ))}
      </ol>
      <div className={styles.actions}>
        <Button variant="secondary" onClick={handleCopy}>
          Copy
        </Button>
        <a className={styles.download} href={downloadURL} download="sentinel-recovery-codes.txt">
          Download
        </a>
      </div>
      <label className={styles.confirm}>
        <input
          type="checkbox"
          checked={hasSaved}
          onChange={(event) => {
            setHasSaved(event.target.checked);
          }}
        />
        I have saved these codes
      </label>
      <Button isFullWidth disabled={!hasSaved} onClick={onDone}>
        Continue
      </Button>
    </section>
  );
}
