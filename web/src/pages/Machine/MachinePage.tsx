// SPDX-License-Identifier: Apache-2.0
import { Link, NavLink, Outlet, useOutletContext, useParams } from 'react-router';
import clsx from 'clsx';

import { isApiError } from '../../api/client';
import { useAgent } from '../../api/agents';
import type { SessionResponse } from '../../api/types.gen';
import { FormError } from '../../components/FormError/FormError';
import { MachineStatus } from '../../components/MachineStatus/MachineStatus';
import { formatRelative } from '../../formatting/formatting';
import { useNow } from '../../hooks/useNow';
import { allows, canOperate, type MachineContext } from './machineContext';
import styles from './MachinePage.module.css';
import { PowerActions } from './PowerActions';
import { WakeAction } from './WakeAction';

/**
 * MachinePage is one machine: its name and status, power actions, and tabs
 * for the overview, terminal, services, processes and logs.
 */
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
        <BackLink />
        {isApiError(agent.error, 'not_found') ? (
          <p className={styles.muted}>This machine doesn’t exist or was revoked.</p>
        ) : (
          <FormError error={agent.error} />
        )}
      </div>
    );
  }

  const machine = agent.data;
  const context: MachineContext = { session, machine };
  return (
    <div className={styles.page}>
      <BackLink />
      <header className={styles.header}>
        <div className={styles.titleRow}>
          <h1 className={styles.title}>{machine.name}</h1>
          {canOperate(session) && allows(machine, 'power') && <PowerActions machine={machine} />}
          {canOperate(session) && !machine.online && machine.canWake && (
            <WakeAction machine={machine} />
          )}
        </div>
        <p className={styles.subtitle}>
          <MachineStatus isOnline={machine.online} />
          {machine.maintenanceUntil !== null && <span className={styles.tag}>In maintenance</span>}
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
      <MachineTabs session={session} machine={machine} />
      <Outlet context={context} />
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/" className={styles.back} aria-label="Back to machines">
      <span aria-hidden="true">←</span> Machines
    </Link>
  );
}

/** MachineTabs links the machine's sections the user may use. */
function MachineTabs({ session, machine }: MachineContext) {
  const tabs = [
    { to: '', label: 'Overview', shown: true },
    {
      to: 'terminal',
      label: 'Terminal',
      shown: canOperate(session) && allows(machine, 'terminal'),
    },
    {
      to: 'desktop',
      label: 'Desktop',
      // Hidden while there's no desktop to share (§9.3).
      shown: canOperate(session) && allows(machine, 'desktop') && machine.desktopAvailable,
    },
    { to: 'services', label: 'Services', shown: true },
    { to: 'processes', label: 'Processes', shown: canOperate(session) },
    { to: 'logs', label: 'Logs', shown: true },
    {
      to: 'updates',
      label: 'Updates',
      // Hidden without a package manager Sentinel supports, or when the
      // local policy switches it off (§10.2).
      shown: canOperate(session) && allows(machine, 'packages'),
    },
    { to: 'files', label: 'Files', shown: canOperate(session) && allows(machine, 'files') },
    { to: 'storage', label: 'Storage', shown: allows(machine, 'storage') },
    { to: 'docker', label: 'Docker', shown: allows(machine, 'docker') },
  ];
  return (
    <nav className={styles.tabs} aria-label="Machine sections">
      {tabs
        .filter((tab) => tab.shown)
        .map((tab) => (
          <NavLink
            key={tab.label}
            to={tab.to}
            end={tab.to !== 'docker'}
            className={({ isActive }) => clsx(styles.tab, isActive && styles.tabActive)}
          >
            {tab.label}
          </NavLink>
        ))}
    </nav>
  );
}
