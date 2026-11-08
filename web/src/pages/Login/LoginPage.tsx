// SPDX-License-Identifier: Apache-2.0
import { useState, type SubmitEvent } from 'react';

import { useLogin } from '../../api/session';
import { AuthLayout } from '../../components/AuthLayout/AuthLayout';
import { Button } from '../../components/Button/Button';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import styles from './LoginPage.module.css';

/**
 * LoginPage is the password step. A correct password leads to the
 * second-factor step; the area gate moves the browser there.
 */
export function LoginPage() {
  const login = useLogin();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');

  const handleSubmit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    login.mutate({ username: username.trim(), password });
  };

  return (
    <AuthLayout title="Sign in">
      <form className={styles.form} onSubmit={handleSubmit}>
        <Field
          label="Username"
          value={username}
          onChange={(event) => {
            setUsername(event.target.value);
          }}
          autoComplete="username"
          autoCapitalize="none"
          autoCorrect="off"
          required
          autoFocus
        />
        <Field
          label="Password"
          type="password"
          value={password}
          onChange={(event) => {
            setPassword(event.target.value);
          }}
          autoComplete="current-password"
          required
        />
        <FormError error={login.error} />
        <Button type="submit" isFullWidth isBusy={login.isPending}>
          Sign in
        </Button>
      </form>
    </AuthLayout>
  );
}
