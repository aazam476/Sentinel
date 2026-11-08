// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { useState } from 'react';
import { useSearchParams } from 'react-router';
import { Virtuoso } from 'react-virtuoso';

import { useLogs, type LogFilter, type LogLevel } from '../../api/system';
import type { MachineLogEntry } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { FormError } from '../../components/FormError/FormError';
import { countOf, formatDateTime } from '../../formatting/formatting';
import { useMachine } from './machineContext';
import machineStyles from './MachinePage.module.css';
import styles from './SystemTab.module.css';

const ranges = [
  { label: '15 min', ms: 15 * 60_000 },
  { label: '1 h', ms: 60 * 60_000 },
  { label: '6 h', ms: 6 * 60 * 60_000 },
  { label: '24 h', ms: 24 * 60 * 60_000 },
];

/** defaultRangeMs is an hour, the agent's own default. */
const defaultRangeMs = 60 * 60_000;

const levelOptions: { value: LogLevel | ''; label: string }[] = [
  { value: '', label: 'All levels' },
  { value: 'error', label: 'Errors' },
  { value: 'warning', label: 'Warnings and errors' },
  { value: 'notice', label: 'Notices and above' },
  { value: 'info', label: 'Info and above' },
];

const levelBadge: Record<LogLevel, string | undefined> = {
  error: styles.error,
  warning: styles.warning,
  notice: undefined,
  info: undefined,
  debug: undefined,
};

/**
 * LogsTab reads the machine's system log, newest first: one service's when
 * the address names it (?service=), otherwise everything.
 */
export function LogsTab() {
  const { machine } = useMachine();
  const [params, setParams] = useSearchParams();
  const service = params.get('service') ?? '';
  const [minLevel, setMinLevel] = useState<LogLevel | ''>('');
  const [sinceMs, setSinceMs] = useState(defaultRangeMs);
  // The window ends when the filter was chosen; Refresh moves it to now.
  const [now, setNow] = useState(() => Date.now());
  const filter: LogFilter = { service, sinceMs, ...(minLevel === '' ? {} : { minLevel }) };
  const logs = useLogs(machine.id, filter, now);

  if (!machine.online) {
    return <p className={styles.muted}>The machine is offline. Its logs show when it’s back.</p>;
  }
  return (
    <section className={styles.tab} aria-label="Logs">
      <div className={styles.toolbar}>
        <input
          type="search"
          className={styles.search}
          placeholder="All services"
          aria-label="Service"
          defaultValue={service}
          autoCapitalize="none"
          autoCorrect="off"
          enterKeyHint="search"
          onChange={(event) => {
            if (event.target.value === '' && service !== '') {
              setParams({}, { replace: true }); // the field's clear button
            }
          }}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              const value = event.currentTarget.value.trim();
              setParams(value === '' ? {} : { service: value }, { replace: true });
            }
          }}
        />
        <select
          className={styles.select}
          aria-label="Level"
          value={minLevel}
          onChange={(event) => {
            setMinLevel(event.target.value as LogLevel | '');
          }}
        >
          {levelOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
        <div className={machineStyles.segmented} role="group" aria-label="Time range">
          {ranges.map((range) => (
            <button
              key={range.label}
              type="button"
              aria-pressed={range.ms === sinceMs}
              className={clsx(
                machineStyles.segment,
                range.ms === sinceMs && machineStyles.segmentActive,
              )}
              onClick={() => {
                setSinceMs(range.ms);
                setNow(Date.now());
              }}
            >
              {range.label}
            </button>
          ))}
        </div>
        <Button
          variant="secondary"
          isBusy={logs.isFetching}
          onClick={() => {
            setNow(Date.now());
            void logs.refetch();
          }}
        >
          Refresh
        </Button>
      </div>
      {logs.isPending ? (
        <p className={styles.muted}>Reading the log…</p>
      ) : logs.data === undefined ? (
        <FormError error={logs.error} />
      ) : (
        <LogList entries={logs.data.entries} isTruncated={logs.data.truncated} />
      )}
    </section>
  );
}

function LogList({ entries, isTruncated }: { entries: MachineLogEntry[]; isTruncated: boolean }) {
  if (entries.length === 0) {
    return <p className={styles.muted}>No log entries in this time range.</p>;
  }
  return (
    <>
      <p className={styles.count}>
        {isTruncated
          ? `The newest ${countOf(entries.length, 'entry', 'entries')}; narrow the time range or level to see older ones.`
          : countOf(entries.length, 'entry', 'entries')}
      </p>
      <Virtuoso
        useWindowScroll
        data={entries}
        className={styles.list}
        itemContent={(_, entry) => (
          <article className={styles.logRow}>
            <span className={styles.meta}>
              <span className={clsx(styles.badge, levelBadge[entry.level])}>{entry.level}</span>
              <time dateTime={new Date(entry.time).toISOString()}>
                {formatDateTime(entry.time)}
              </time>
              {entry.source !== '' && <span className={styles.name}>{entry.source}</span>}
            </span>
            <p className={styles.logMessage}>{entry.message}</p>
          </article>
        )}
      />
    </>
  );
}
