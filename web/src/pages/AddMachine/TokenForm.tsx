// SPDX-License-Identifier: Apache-2.0
import { type SubmitEvent, useState } from 'react';

import type { CreateEnrollmentTokenRequest } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import styles from './AddMachinePage.module.css';

interface TokenFormProps {
  isBusy: boolean;
  onSubmit: (request: CreateEnrollmentTokenRequest) => void;
}

const lifetimes = [
  { hours: 1, label: '1 hour' },
  { hours: 24, label: '24 hours' },
  { hours: 168, label: '7 days' },
];

/** TokenForm asks for the few choices an enrollment token has. */
export function TokenForm({ isBusy, onSubmit }: TokenFormProps) {
  const [note, setNote] = useState('');
  const [expiresInHours, setExpiresInHours] = useState(24);
  const [maxUses, setMaxUses] = useState('1');

  const handleSubmit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    onSubmit({ note, expiresInHours, maxUses: Number(maxUses) || 1 });
  };

  return (
    <form className={styles.form} onSubmit={handleSubmit}>
      <label className={styles.field}>
        <span className={styles.label}>Note (optional)</span>
        <input
          className={styles.input}
          value={note}
          maxLength={100}
          placeholder="e.g. office NAS"
          onChange={(event) => {
            setNote(event.target.value);
          }}
        />
      </label>
      <div className={styles.row}>
        <label className={styles.field}>
          <span className={styles.label}>Valid for</span>
          <select
            className={styles.input}
            value={expiresInHours}
            onChange={(event) => {
              setExpiresInHours(Number(event.target.value));
            }}
          >
            {lifetimes.map((lifetime) => (
              <option key={lifetime.hours} value={lifetime.hours}>
                {lifetime.label}
              </option>
            ))}
          </select>
        </label>
        <label className={styles.field}>
          <span className={styles.label}>Machines it can add</span>
          <input
            className={styles.input}
            type="number"
            inputMode="numeric"
            min={1}
            max={1000}
            required
            value={maxUses}
            onChange={(event) => {
              setMaxUses(event.target.value);
            }}
          />
        </label>
      </div>
      <Button type="submit" isBusy={isBusy}>
        Create install command
      </Button>
    </form>
  );
}
