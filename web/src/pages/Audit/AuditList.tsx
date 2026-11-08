// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { Virtuoso } from 'react-virtuoso';

import type { AuditEntry } from '../../api/types.gen';
import { formatDateTime } from '../../formatting/formatting';
import styles from './AuditPage.module.css';

interface AuditListProps {
  entries: AuditEntry[];
  onEndReached: () => void;
}

/**
 * AuditList renders the log with only the visible rows in the page, so it
 * stays fast with any number of entries. It scrolls with the page.
 */
export function AuditList({ entries, onEndReached }: AuditListProps) {
  if (entries.length === 0) {
    return <p className={styles.text}>No entries yet.</p>;
  }
  return (
    <Virtuoso
      useWindowScroll
      data={entries}
      endReached={onEndReached}
      computeItemKey={(_, entry) => entry.id}
      className={styles.list}
      itemContent={(_, entry) => <AuditRow entry={entry} />}
    />
  );
}

function AuditRow({ entry }: { entry: AuditEntry }) {
  const details = Object.entries(entry.details)
    .map(([key, value]) => `${key}: ${typeof value === 'string' ? value : JSON.stringify(value)}`)
    .join(', ');

  return (
    <article className={styles.row} aria-label={`Entry ${String(entry.id)}: ${entry.action}`}>
      <div className={styles.rowMain}>
        <span className={styles.action}>{entry.action}</span>
        <span className={clsx(styles.outcome, styles[entry.outcome])}>{entry.outcome}</span>
      </div>
      <div className={styles.rowMeta}>
        <time dateTime={new Date(entry.time).toISOString()}>{formatDateTime(entry.time)}</time>
        <span>{entry.username === '' ? 'No user' : entry.username}</span>
        <span>{entry.ip}</span>
        <span>#{entry.id}</span>
      </div>
      {details !== '' && <div className={styles.details}>{details}</div>}
    </article>
  );
}
