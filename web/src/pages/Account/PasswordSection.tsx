// SPDX-License-Identifier: Apache-2.0
import { type SubmitEvent, useState } from 'react';
import { toast } from 'sonner';

import { isApiError } from '../../api/client';
import { useChangePassword } from '../../api/users';
import { Button } from '../../components/Button/Button';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import styles from './AccountPage.module.css';

/**
 * PasswordSection changes the user's own password. Their other sessions
 * end, so anyone else who knew the old one is signed out.
 */
export function PasswordSection() {
  const change = useChangePassword();
  const [current, setCurrent] = useState('');
  const [replacement, setReplacement] = useState('');
  const [repeated, setRepeated] = useState('');
  const mismatch = repeated !== '' && repeated !== replacement;

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    change.mutate(
      { currentPassword: current, newPassword: replacement },
      {
        onSuccess: () => {
          setCurrent('');
          setReplacement('');
          setRepeated('');
          toast.success('Password changed. Your other sessions were signed out.');
        },
      },
    );
  };
  const wrongCurrent = isApiError(change.error, 'invalid_credentials');
  const newProblem = isApiError(change.error, 'invalid_input') ? change.error.message : undefined;

  return (
    <section className={styles.section} aria-labelledby="password-title">
      <h2 id="password-title" className={styles.sectionTitle}>
        Password
      </h2>
      <form className={styles.form} onSubmit={submit}>
        <Field
          label="Current password"
          type="password"
          autoComplete="current-password"
          value={current}
          error={wrongCurrent ? 'That isn’t your current password.' : undefined}
          onChange={(event) => {
            setCurrent(event.target.value);
          }}
        />
        <Field
          label="New password"
          type="password"
          autoComplete="new-password"
          hint="At least 12 characters."
          value={replacement}
          error={newProblem}
          onChange={(event) => {
            setReplacement(event.target.value);
          }}
        />
        <Field
          label="New password again"
          type="password"
          autoComplete="new-password"
          value={repeated}
          error={mismatch ? 'The two new passwords are different.' : undefined}
          onChange={(event) => {
            setRepeated(event.target.value);
          }}
        />
        <FormError error={wrongCurrent || newProblem !== undefined ? null : change.error} />
        <div>
          <Button
            type="submit"
            isBusy={change.isPending}
            disabled={current === '' || replacement === '' || repeated !== replacement}
          >
            Change password
          </Button>
        </div>
      </form>
    </section>
  );
}
