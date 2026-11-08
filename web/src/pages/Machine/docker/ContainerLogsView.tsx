// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { useDeferredValue, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router';
import { Virtuoso } from 'react-virtuoso';

import { apiURL } from '../../../api/client';
import { followLogsPath } from '../../../api/docker';
import type { ContainerLogLine } from '../../../api/protocol.gen';
import { Button } from '../../../components/Button/Button';
import { countOf, formatDateTime } from '../../../formatting/formatting';
import { useMachine } from '../machineContext';
import styles from '../SystemTab.module.css';

/** maxLines is how many lines the page keeps; older ones scroll away. */
const maxLines = 5000;

type FollowState = 'connecting' | 'following' | 'paused' | 'ended';

/**
 * ContainerLogsView shows a container's log and follows it live (the
 * server's event stream of the container's lines), with a filter.
 */
export function ContainerLogsView() {
  const { machine } = useMachine();
  const { container = '' } = useParams();
  const [lines, setLines] = useState<ContainerLogLine[]>([]);
  const [state, setState] = useState<FollowState>('connecting');
  const [endReason, setEndReason] = useState('');
  const [search, setSearch] = useState('');
  const deferredSearch = useDeferredValue(search.trim().toLowerCase());

  const isStopped = state === 'paused' || state === 'ended';
  // An effect because the event stream is a connection outside React,
  // closed when the view closes or following is paused.
  useEffect(() => {
    if (isStopped || typeof EventSource === 'undefined') {
      return undefined;
    }
    const source = new EventSource(apiURL(followLogsPath(machine.id, container)), {
      withCredentials: true,
    });
    source.addEventListener('open', () => {
      setLines([]); // the stream starts with the recent lines again
      setState('following');
    });
    source.addEventListener('line', (event) => {
      const line = JSON.parse((event as MessageEvent<string>).data) as ContainerLogLine;
      setLines((current) => [...current.slice(-(maxLines - 1)), line]);
    });
    source.addEventListener('end', (event) => {
      const { reason } = JSON.parse((event as MessageEvent<string>).data) as { reason: string };
      setEndReason(reason);
      setState('ended');
      source.close();
    });
    return () => {
      source.close();
    };
  }, [machine.id, container, isStopped]);

  const shown =
    deferredSearch === ''
      ? lines
      : lines.filter((line) => line.text.toLowerCase().includes(deferredSearch));
  return (
    <section className={styles.tab} aria-label={`Logs of ${container}`}>
      <Link to=".." className={styles.muted}>
        ← Containers
      </Link>
      <div className={styles.toolbar}>
        <input
          type="search"
          className={styles.search}
          placeholder="Filter lines"
          aria-label="Filter lines"
          value={search}
          autoCapitalize="none"
          autoCorrect="off"
          onChange={(event) => {
            setSearch(event.target.value);
          }}
        />
        {state === 'following' || state === 'connecting' ? (
          <Button
            variant="secondary"
            onClick={() => {
              setState('paused');
            }}
          >
            Pause
          </Button>
        ) : (
          <Button
            variant="secondary"
            onClick={() => {
              setState('connecting');
            }}
          >
            {state === 'ended' ? 'Follow again' : 'Resume'}
          </Button>
        )}
      </div>
      <p className={styles.count} role="status">
        {state === 'ended'
          ? `Stopped following: ${endReason}`
          : state === 'paused'
            ? 'Paused'
            : state === 'connecting'
              ? 'Connecting…'
              : `Following · ${countOf(lines.length, 'line', 'lines')}`}
      </p>
      {shown.length > 0 && (
        <Virtuoso
          useWindowScroll
          data={shown}
          followOutput={state === 'following' ? 'auto' : false}
          className={styles.list}
          itemContent={(_, line) => (
            <article className={styles.logRow}>
              <span className={styles.meta}>
                <span className={clsx(styles.badge, line.stream === 'stderr' && styles.warning)}>
                  {line.stream}
                </span>
                {line.time > 0 && (
                  <time dateTime={new Date(line.time).toISOString()}>
                    {formatDateTime(line.time)}
                  </time>
                )}
              </span>
              <p className={styles.logMessage}>{line.text}</p>
            </article>
          )}
        />
      )}
    </section>
  );
}
