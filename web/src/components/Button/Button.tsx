// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import type { ButtonHTMLAttributes } from 'react';

import styles from './Button.module.css';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost';
  /** isBusy disables the button and tells screen readers work is under way. */
  isBusy?: boolean;
  isFullWidth?: boolean;
}

/** Button is every clickable action in the app. */
export function Button({
  variant = 'primary',
  isBusy = false,
  isFullWidth = false,
  className,
  disabled,
  type = 'button',
  children,
  ...rest
}: ButtonProps) {
  return (
    <button
      {...rest}
      type={type}
      disabled={disabled === true || isBusy}
      aria-busy={isBusy || undefined}
      className={clsx(styles.button, styles[variant], isFullWidth && styles.fullWidth, className)}
    >
      {children}
    </button>
  );
}
