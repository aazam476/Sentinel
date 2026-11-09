// SPDX-License-Identifier: Apache-2.0
import { Link } from 'react-router';

import { useScripts } from '../../api/scripts';
import { FormError } from '../../components/FormError/FormError';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from './Scripts.module.css';
import { languageNames, systemNames } from './scriptText';

/** LibraryView lists the scripts, by name. */
export function LibraryView() {
  const scripts = useScripts();
  return (
    <section className={styles.section} aria-label="Script library">
      <div className={systemStyles.toolbar}>
        <Link to="/scripts/new" className={styles.primaryLink}>
          New script
        </Link>
      </div>
      {scripts.isPending && <p className={styles.muted}>Loading the library…</p>}
      {scripts.isError && <FormError error={scripts.error} />}
      {scripts.data?.scripts.length === 0 && (
        <p className={styles.muted}>
          No scripts yet. Write one to run it on any machine, or on a schedule.
        </p>
      )}
      {scripts.data !== undefined && scripts.data.scripts.length > 0 && (
        <ul className={systemStyles.list}>
          {scripts.data.scripts.map((script) => (
            <li key={script.id} className={styles.row}>
              <Link to={`/scripts/${String(script.id)}`} className={styles.name}>
                {script.name}
              </Link>
              {script.description !== '' && (
                <span className={styles.description}>{script.description}</span>
              )}
              <span className={systemStyles.meta}>
                <span>{languageNames[script.language] ?? script.language}</span>
                <span>{systemNames[script.targetOS] ?? script.targetOS}</span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
