// SPDX-License-Identifier: Apache-2.0
import { Link, useOutletContext, useParams } from 'react-router';

import { isApiError } from '../../api/client';
import { useAgent } from '../../api/agents';
import { metricNames } from '../../api/metricNames';
import type { SessionResponse } from '../../api/types.gen';
import { FormError } from '../../components/FormError/FormError';
import { MachineStatus } from '../../components/MachineStatus/MachineStatus';
import { formatPercent, formatRelative } from '../../formatting/formatting';
import { useNow } from '../../hooks/useNow';
import { Capabilities } from './Capabilities';
import { Filesystems } from './Filesystems';
import { HistoryChart } from './HistoryChart';
import { LiveChart } from './LiveChart';
import { MachineSettings } from './MachineSettings';
import styles from './MachinePage.module.css';
import { SystemDetails } from './SystemDetails';

/** MachinePage is one machine: live and historical charts, details and settings. */
export function MachinePage() {
  const { id = '' } = useParams();
  const session = useOutletContext<SessionResponse>();
  const agent = useAgent(id);
  const now = useNow();

  if (agent.isPending) {
    return <p className={styles.muted}>Loading…</p>;
  }
  if (agent.data === undefined) {
    return (
      <div className={styles.page}>
        <Link to="/" className={styles.back} aria-label="Back to machines">
          <span aria-hidden="true">←</span> Machines
        </Link>
        {isApiError(agent.error, 'not_found') ? (
          <p className={styles.muted}>This machine doesn’t exist or was revoked.</p>
        ) : (
          <FormError error={agent.error} />
        )}
      </div>
    );
  }

  const machine = agent.data;
  return (
    <div className={styles.page}>
      <Link to="/" className={styles.back} aria-label="Back to machines">
        <span aria-hidden="true">←</span> Machines
      </Link>
      <header className={styles.header}>
        <h1 className={styles.title}>{machine.name}</h1>
        <p className={styles.subtitle}>
          <MachineStatus isOnline={machine.online} />
          {!machine.online && machine.lastSeenAt !== null && (
            <span className={styles.muted}>
              Last seen {formatRelative(machine.lastSeenAt, now)}
            </span>
          )}
          {machine.tags.map((tag) => (
            <span key={tag} className={styles.tag} title={tag}>
              {tag}
            </span>
          ))}
        </p>
      </header>
      <div className={styles.liveGrid}>
        <LiveChart
          agentId={machine.id}
          metric={metricNames.cpu}
          title="CPU"
          current={machine.current}
          formatValue={formatPercent}
        />
        <LiveChart
          agentId={machine.id}
          metric={metricNames.memory}
          title="Memory"
          current={machine.current}
          formatValue={formatPercent}
        />
      </div>
      <HistoryChart agentId={machine.id} />
      <div className={styles.panelGrid}>
        <SystemDetails agent={machine} />
        <Capabilities capabilities={machine.capabilities} />
      </div>
      <Filesystems filesystems={machine.filesystems} isOnline={machine.online} />
      {session.user.role === 'admin' && <MachineSettings key={machine.id} agent={machine} />}
    </div>
  );
}
