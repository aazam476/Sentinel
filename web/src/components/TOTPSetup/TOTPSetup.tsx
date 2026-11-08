// SPDX-License-Identifier: Apache-2.0
import { useState, type SubmitEvent } from 'react';

import type { TOTPEnrollmentResponse } from '../../api/types.gen';
import { Button } from '../Button/Button';
import { CodeField } from '../Field/CodeField';
import { FormError } from '../FormError/FormError';
import styles from './TOTPSetup.module.css';

interface TOTPSetupProps {
  enrollment: TOTPEnrollmentResponse;
  isBusy: boolean;
  error: unknown;
  onConfirm: (code: string) => void;
}

/**
 * TOTPSetup shows the QR code and secret for an authenticator app and asks
 * for the first code, which proves the app is set up correctly.
 */
export function TOTPSetup({ enrollment, isBusy, error, onConfirm }: TOTPSetupProps) {
  const [code, setCode] = useState('');

  const handleSubmit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    onConfirm(code.trim());
  };

  return (
    <form className={styles.form} onSubmit={handleSubmit}>
      <ol className={styles.steps}>
        <li>Open your authenticator app and scan this code.</li>
        <li>Enter the 6-digit code it shows.</li>
      </ol>
      <img
        className={styles.qr}
        src={enrollment.qrCode}
        alt="QR code for your authenticator app"
        width={192}
        height={192}
      />
      <details className={styles.manual}>
        <summary>Can’t scan it?</summary>
        <p>Enter this key in your app instead:</p>
        <code className={styles.secret}>{enrollment.secret}</code>
      </details>
      <CodeField
        value={code}
        onChange={(event) => {
          setCode(event.target.value);
        }}
        autoFocus
      />
      <FormError error={error} />
      <Button type="submit" isFullWidth isBusy={isBusy}>
        Confirm
      </Button>
    </form>
  );
}
