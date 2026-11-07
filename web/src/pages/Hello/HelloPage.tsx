// SPDX-License-Identifier: Apache-2.0
import styles from './HelloPage.module.css';

/**
 * HelloPage is the placeholder screen the server shows before any real
 * features exist. It proves the build pipeline end to end: Vite builds it,
 * the Go server embeds and serves it, and the end-to-end tests load it.
 */
export function HelloPage() {
  return (
    <main className={styles.page}>
      <div className={styles.content}>
        <p className={styles.wordmark}>Sentinel</p>
        <h1 className={styles.heading}>Hello Sentinel</h1>
        <p className={styles.status}>The server is running.</p>
      </div>
    </main>
  );
}
