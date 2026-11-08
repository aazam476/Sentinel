// SPDX-License-Identifier: Apache-2.0
import styles from './LoadingScreen.module.css';

/** LoadingScreen fills the page while the first data loads. */
export function LoadingScreen() {
  return (
    <main className={styles.page} aria-busy="true">
      <p className={styles.text}>Loading…</p>
    </main>
  );
}
