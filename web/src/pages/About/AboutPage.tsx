// SPDX-License-Identifier: Apache-2.0
import { useAbout } from '../../api/about';
import { FormError } from '../../components/FormError/FormError';
import { countOf, formatBytes } from '../../formatting/formatting';
import machineStyles from '../Machine/MachinePage.module.css';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from '../Scripts/Scripts.module.css';

/** AboutPage shows the server's version, its database size and which agent versions the fleet runs (§9.11). */
export function AboutPage() {
  const about = useAbout();
  const facts: [string, string][] =
    about.data === undefined
      ? []
      : [
          ['Server version', about.data.serverVersion],
          ['Database size', formatBytes(about.data.databaseBytes)],
          ['Machines', countOf(about.data.machines, 'machine', 'machines')],
        ];

  return (
    <div className={styles.page}>
      <h1 className={styles.title}>About Sentinel</h1>
      {about.isPending && <p className={styles.muted}>Loading…</p>}
      <FormError error={about.error} />
      {facts.length > 0 && about.data !== undefined && (
        <>
          <section className={styles.section} aria-label="Server">
            <dl className={machineStyles.details}>
              {facts.map(([term, value]) => (
                <div key={term} className={machineStyles.detail}>
                  <dt>{term}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
          </section>
          <section className={styles.section} aria-labelledby="agent-versions">
            <h2 id="agent-versions" className={styles.subtitle}>
              Agent versions
            </h2>
            {about.data.agentVersions.length === 0 ? (
              <p className={styles.muted}>No machines yet.</p>
            ) : (
              <ul className={systemStyles.list}>
                {about.data.agentVersions.map((entry) => (
                  <li key={entry.version} className={styles.row} aria-label={entry.version}>
                    <span className={styles.name}>{entry.version}</span>
                    <span className={systemStyles.meta}>
                      {countOf(entry.machines, 'machine', 'machines')}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}
    </div>
  );
}
