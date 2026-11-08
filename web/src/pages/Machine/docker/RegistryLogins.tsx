// SPDX-License-Identifier: Apache-2.0
import { useState, type SubmitEvent } from 'react';
import { toast } from 'sonner';

import {
  useAddRegistryLogin,
  useRegistryLogins,
  useRemoveRegistryLogin,
} from '../../../api/docker';
import { Button } from '../../../components/Button/Button';
import { Field } from '../../../components/Field/Field';
import { FormError } from '../../../components/FormError/FormError';
import { messageFor } from '../../../components/FormError/messageFor';
import styles from '../SystemTab.module.css';

/**
 * RegistryLogins manages the logins Sentinel uses to pull private images
 * (admins only). Passwords are stored encrypted and never shown again.
 */
export function RegistryLogins() {
  const logins = useRegistryLogins(true);
  const add = useAddRegistryLogin();
  const remove = useRemoveRegistryLogin();
  const [form, setForm] = useState({ serverAddress: '', username: '', password: '' });

  const handleAdd = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    add.mutate(form, {
      onSuccess: () => {
        toast.success(`Saved the login for ${form.serverAddress}`);
        setForm({ serverAddress: '', username: '', password: '' });
      },
    });
  };
  const field = (name: keyof typeof form) => (event: { target: { value: string } }) => {
    setForm((current) => ({ ...current, [name]: event.target.value }));
  };
  return (
    <section className={styles.tab} aria-labelledby="registry-logins">
      <h2 id="registry-logins" className={styles.name}>
        Registry logins
      </h2>
      <p className={styles.muted}>
        Used to pull private images and check them for updates, on every machine. Passwords are
        stored encrypted and never shown again.
      </p>
      <ul className={styles.list}>
        {(logins.data?.logins ?? []).map((login) => (
          <li key={login.id} className={styles.row}>
            <span className={styles.rowMain}>
              <span className={styles.name}>{login.serverAddress}</span>
              <span className={styles.description}>{login.username}</span>
            </span>
            <Button
              variant="ghost"
              onClick={() => {
                remove.mutate(login.id, { onError: (error) => toast.error(messageFor(error)) });
              }}
            >
              Remove
            </Button>
          </li>
        ))}
      </ul>
      <form className={styles.toolbar} onSubmit={handleAdd}>
        <Field
          label="Registry"
          value={form.serverAddress}
          onChange={field('serverAddress')}
          placeholder="ghcr.io"
        />
        <Field
          label="User name"
          value={form.username}
          onChange={field('username')}
          autoComplete="off"
        />
        <Field
          label="Password or token"
          type="password"
          value={form.password}
          onChange={field('password')}
          autoComplete="new-password"
        />
        <Button type="submit" variant="secondary" isBusy={add.isPending}>
          Add login
        </Button>
      </form>
      <FormError error={add.error ?? logins.error} />
    </section>
  );
}
