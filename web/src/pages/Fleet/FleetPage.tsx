// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { Link, useOutletContext } from 'react-router';
import { Virtuoso } from 'react-virtuoso';

import { useAgents } from '../../api/agents';
import type { Agent, SessionResponse } from '../../api/types.gen';
import { FormError } from '../../components/FormError/FormError';
import { countOf, formatNumber } from '../../formatting/formatting';
import { useNow } from '../../hooks/useNow';
import { filterAgents } from './filterAgents';
import { FleetToolbar } from './FleetToolbar';
import { MachineRow } from './MachineRow';
import styles from './FleetPage.module.css';

/**
 * FleetPage is the signed-in home page: every machine, online ones with
 * their current CPU, memory and fullest disk. The numbers stay live
 * through the event stream (useFleetEvents in the app shell).
 */
export function FleetPage() {
  const session = useOutletContext<SessionResponse>();
  const agents = useAgents();
  const [search, setSearch] = useState('');
  const [selectedTag, setSelectedTag] = useState<string | null>(null);
  const isAdmin = session.user.role === 'admin';

  const all = agents.data?.agents ?? [];
  const tags = [...new Set(all.flatMap((agent) => agent.tags))].sort();
  const shown = filterAgents(all, search, selectedTag);
  const online = all.filter((agent) => agent.online).length;

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <div>
          <h1 className={styles.title}>Machines</h1>
          {all.length > 0 && (
            <p className={styles.summary}>
              {countOf(all.length, 'machine', 'machines')} · {formatNumber(online)} online
            </p>
          )}
        </div>
        {isAdmin && (
          <Link to="/machines/add" className={styles.addButton}>
            Add machine
          </Link>
        )}
      </div>
      <FormError error={agents.error} />
      {agents.isPending ? (
        <p className={styles.summary}>Loading…</p>
      ) : all.length === 0 ? (
        <EmptyFleet isAdmin={isAdmin} />
      ) : (
        <>
          <FleetToolbar
            search={search}
            onSearchChange={setSearch}
            tags={tags}
            selectedTag={selectedTag}
            onTagChange={setSelectedTag}
          />
          <MachineList agents={shown} />
        </>
      )}
    </div>
  );
}

function MachineList({ agents }: { agents: Agent[] }) {
  const now = useNow();
  if (agents.length === 0) {
    return <p className={styles.summary}>No machines match.</p>;
  }
  return (
    <Virtuoso
      useWindowScroll
      data={agents}
      computeItemKey={(_, agent) => agent.id}
      className={styles.list}
      itemContent={(_, agent) => <MachineRow agent={agent} now={now} />}
    />
  );
}

function EmptyFleet({ isAdmin }: { isAdmin: boolean }) {
  return (
    <section className={styles.empty} aria-labelledby="no-machines">
      <h2 id="no-machines" className={styles.emptyTitle}>
        No machines yet
      </h2>
      <p className={styles.emptyText}>
        {isAdmin
          ? 'Add a machine to install the agent on it. It appears here as soon as it connects.'
          : 'An admin can add machines. They appear here as soon as they connect.'}
      </p>
    </section>
  );
}
