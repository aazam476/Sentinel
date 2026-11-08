// SPDX-License-Identifier: Apache-2.0
import type { InputHTMLAttributes } from 'react';

import { Field } from './Field';

interface CodeFieldProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
}

/**
 * CodeField is a 6-digit authenticator code input. The numeric keyboard
 * opens on phones, and the one-time-code hint lets browsers and password
 * managers fill it in.
 */
export function CodeField({ label = 'Authenticator code', error, ...input }: CodeFieldProps) {
  return (
    <Field
      {...input}
      label={label}
      error={error}
      hint="The 6-digit code from your authenticator app."
      inputMode="numeric"
      autoComplete="one-time-code"
      pattern="[0-9]{6}"
      maxLength={6}
      required
    />
  );
}
