// SPDX-License-Identifier: Apache-2.0
import { useState, type SubmitEvent } from 'react';

import { isApiError } from '../../api/client';
import { Button } from '../../components/Button/Button';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import styles from './SetupDetailsForm.module.css';

export interface SetupDetails {
  setupToken: string;
  username: string;
  password: string;
}

interface SetupDetailsFormProps {
  isBusy: boolean;
  error: unknown;
  onSubmit: (details: SetupDetails) => void;
}

/** SetupDetailsForm collects the setup token and the first admin's login. */
export function SetupDetailsForm({ isBusy, error, onSubmit }: SetupDetailsFormProps) {
  const [setupToken, setSetupToken] = useState('');
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [hasTriedSubmit, setHasTriedSubmit] = useState(false);

  const passwordsDiffer = hasTriedSubmit && password !== confirmation;
  const fieldError = (field: string) =>
    isApiError(error, 'invalid_input') && error.field === field ? error.message : undefined;
  const tokenError = isApiError(error, 'invalid_setup_token')
    ? 'This setup token is incorrect.'
    : undefined;
  const generalError =
    isApiError(error, 'invalid_input') || tokenError !== undefined ? null : error;

  const handleSubmit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    setHasTriedSubmit(true);
    if (password !== confirmation) {
      return;
    }
    onSubmit({ setupToken: setupToken.trim(), username: username.trim(), password });
  };

  return (
    <form className={styles.form} onSubmit={handleSubmit} noValidate>
      <Field
        label="Setup token"
        value={setupToken}
        onChange={(event) => {
          setSetupToken(event.target.value);
        }}
        error={tokenError}
        autoComplete="off"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        required
      />
      <Field
        label="Admin username"
        value={username}
        onChange={(event) => {
          setUsername(event.target.value);
        }}
        error={fieldError('username')}
        autoComplete="username"
        autoCapitalize="none"
        autoCorrect="off"
        required
      />
      <Field
        label="Password"
        type="password"
        value={password}
        onChange={(event) => {
          setPassword(event.target.value);
        }}
        error={fieldError('password')}
        hint="At least 12 characters, and not a commonly used password."
        autoComplete="new-password"
        required
      />
      <Field
        label="Confirm password"
        type="password"
        value={confirmation}
        onChange={(event) => {
          setConfirmation(event.target.value);
        }}
        error={passwordsDiffer ? 'The passwords don’t match.' : undefined}
        autoComplete="new-password"
        required
      />
      <FormError error={generalError} />
      <Button type="submit" isFullWidth isBusy={isBusy}>
        Continue
      </Button>
    </form>
  );
}
