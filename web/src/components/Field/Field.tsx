// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { useId, type InputHTMLAttributes } from 'react';

import styles from './Field.module.css';

interface FieldProps extends InputHTMLAttributes<HTMLInputElement> {
  label: string;
  /** hint explains the field, under it. */
  hint?: string;
  /** error replaces the hint and marks the field invalid. */
  error?: string;
}

/** Field is a labelled text input with an optional hint or error. */
export function Field({ label, hint, error, className, ...input }: FieldProps) {
  const id = useId();
  const describedBy = error !== undefined || hint !== undefined ? `${id}-description` : undefined;

  return (
    <div className={clsx(styles.field, className)}>
      <label className={styles.label} htmlFor={id}>
        {label}
      </label>
      <input
        {...input}
        id={id}
        className={clsx(styles.input, error !== undefined && styles.invalid)}
        aria-invalid={error !== undefined || undefined}
        aria-describedby={describedBy}
      />
      {error !== undefined ? (
        <p id={describedBy} className={styles.error}>
          {error}
        </p>
      ) : (
        hint !== undefined && (
          <p id={describedBy} className={styles.hint}>
            {hint}
          </p>
        )
      )}
    </div>
  );
}
