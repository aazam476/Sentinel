// SPDX-License-Identifier: Apache-2.0
import { Link } from 'react-router';

import { useBatches } from '../../api/scripts';
import { FormError } from '../../components/FormError/FormError';
import { formatDateTime, formatNumber } from '../../formatting/formatting';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from './Scripts.module.css';
import { RunState } from './StateBadge';

/** RunsView lists recent script runs, newest first. */
export function RunsView() {
  const batches = useBatches();
  return (
    <section className={styles.section} aria-label="Script runs">
      {batches.isPending && <p className={styles.muted}>Loading runs…</p>}
      {batches.isError && <FormError error={batches.error} />}
      {batches.data?.batches.length === 0 && (
        <p className={styles.muted}>No scripts have run yet.</p>
      )}
      {batches.data !== undefined && batches.data.batches.length > 0 && (
        <ul className={systemStyles.list}>
          {batches.data.batches.map((batch) => (
            <li
              key={batch.id}
              className={styles.row}
              aria-label={`${batch.scriptName}, ${formatDateTime(batch.startedAt)}`}
            >
              <Link to={`/scripts/runs/${String(batch.id)}`} className={styles.name}>
                {batch.scriptName}
              </Link>
              <span className={systemStyles.meta}>
                <span>{formatDateTime(batch.startedAt)}</span>
                <span>{batch.target}</span>
                <span>{batch.startedBy !== '' ? `by ${batch.startedBy}` : 'on a schedule'}</span>
              </span>
              <span className={styles.counts}>
                {Object.entries(batch.counts).map(([state, count]) => (
                  <span key={state} className={styles.count}>
                    <RunState state={state} /> {formatNumber(count)}
                  </span>
                ))}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
