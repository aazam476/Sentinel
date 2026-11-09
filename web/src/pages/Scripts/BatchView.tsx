// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { Link, useParams } from 'react-router';

import { isRunning, useBatch, useCancelRun, useRunOutput } from '../../api/scripts';
import type { ScriptRun } from '../../api/types.gen';
import { FormError } from '../../components/FormError/FormError';
import { formatDateTime, formatNumber, formatUptime } from '../../formatting/formatting';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from './Scripts.module.css';
import { RunState } from './StateBadge';

/** BatchView is one script run: each machine's state, exit code and output. */
export function BatchView() {
  const { batchId = '' } = useParams();
  const batch = useBatch(Number(batchId));
  const [open, setOpen] = useState<number>();
  if (batch.isPending) {
    return <p className={styles.muted}>Loading the run…</p>;
  }
  if (batch.data === undefined) {
    return <FormError error={batch.error} />;
  }
  const { data } = batch;
  return (
    <section className={styles.section} aria-label={`Run of ${data.scriptName}`}>
      <Link to="/scripts/runs" className={styles.muted}>
        ← Runs
      </Link>
      <h2 className={styles.subtitle}>{data.scriptName}</h2>
      <p className={styles.muted}>
        {formatDateTime(data.startedAt)} · {data.target} ·{' '}
        {data.startedBy !== '' ? `by ${data.startedBy}` : 'on a schedule'}
      </p>
      <ul className={systemStyles.list}>
        {data.runs.map((run) => (
          <RunRow
            key={run.id}
            run={run}
            isOpen={open === run.id || data.runs.length === 1}
            onToggle={() => {
              setOpen(open === run.id ? undefined : run.id);
            }}
          />
        ))}
      </ul>
      <details className={styles.script}>
        <summary>The script as it ran</summary>
        <pre className={styles.output}>{data.body}</pre>
      </details>
    </section>
  );
}

function RunRow({
  run,
  isOpen,
  onToggle,
}: {
  run: ScriptRun;
  isOpen: boolean;
  onToggle: () => void;
}) {
  const duration =
    run.finishedAt === null ? undefined : Math.round((run.finishedAt - run.startedAt) / 1000);
  return (
    <li className={styles.row} aria-label={run.agentName}>
      <span className={styles.runHeader}>
        <button
          type="button"
          className={systemStyles.linkButton}
          aria-expanded={isOpen}
          onClick={onToggle}
        >
          <span className={styles.name}>{run.agentName}</span>
        </button>
        <RunState state={run.state} />
        {run.exitCode !== null && (
          <span className={styles.muted}>exit {formatNumber(run.exitCode)}</span>
        )}
        {duration !== undefined && (
          <span className={styles.muted}>
            {duration < 60 ? `${String(duration)} s` : formatUptime(duration)}
          </span>
        )}
      </span>
      {run.error !== '' && <span className={styles.problem}>{run.error}</span>}
      {isOpen && <RunOutputView runId={run.id} />}
    </li>
  );
}

function RunOutputView({ runId }: { runId: number }) {
  const output = useRunOutput(runId);
  const cancel = useCancelRun();
  if (output.data === undefined) {
    return output.isError ? (
      <FormError error={output.error} />
    ) : (
      <p className={styles.muted}>Loading the output…</p>
    );
  }
  const running = isRunning(output.data.state);
  return (
    <div className={styles.outputBox}>
      {running && (
        <button
          type="button"
          className={systemStyles.linkButton}
          onClick={() => {
            cancel.mutate(runId);
          }}
        >
          Stop it
        </button>
      )}
      <pre className={styles.output} aria-label="Output" tabIndex={0}>
        {output.data.output === ''
          ? running
            ? 'Waiting for output…'
            : 'No output.'
          : output.data.output}
      </pre>
      {output.data.truncated && (
        <p className={styles.muted}>Only the first 1 MB of output was kept.</p>
      )}
    </div>
  );
}
