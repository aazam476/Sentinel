// SPDX-License-Identifier: Apache-2.0
import type { ReactNode } from 'react';

import styles from './AuthLayout.module.css';

interface AuthLayoutProps {
  title: string;
  description?: ReactNode;
  children: ReactNode;
}

/** AuthLayout is the centered card used by setup and sign-in screens. */
export function AuthLayout({ title, description, children }: AuthLayoutProps) {
  return (
    <main className={styles.page}>
      <div className={styles.card}>
        <p className={styles.wordmark}>Sentinel</p>
        <h1 className={styles.title}>{title}</h1>
        {description !== undefined && <div className={styles.description}>{description}</div>}
        {children}
      </div>
    </main>
  );
}
