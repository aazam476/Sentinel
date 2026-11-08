// SPDX-License-Identifier: Apache-2.0
import { Link } from 'react-router';

import { metricNames } from '../../api/metricNames';
import type { Agent } from '../../api/types.gen';
import { MachineStatus } from '../../components/MachineStatus/MachineStatus';
import { countOf, formatRelative, formatUptime } from '../../formatting/formatting';
import { MetricMeter } from './MetricMeter';
import styles from './FleetPage.module.css';

interface MachineRowProps {
  agent: Agent;
  now: number;
}

/** MachineRow is one machine in the fleet list; the whole row opens it. */
export function MachineRow({ agent, now }: MachineRowProps) {
  const values = agent.current?.values ?? {};
  const platform = [agent.platform || agent.os, agent.platformVersion].filter(Boolean).join(' ');
  return (
    <Link to={`/machines/${encodeURIComponent(agent.id)}`} className={styles.row}>
      <div className={styles.identity}>
        <span className={styles.nameLine}>
          <MachineStatus isOnline={agent.online} isCompact />
          <span className={styles.name} title={agent.name}>
            {agent.name}
          </span>
          {agent.firingAlerts > 0 && (
            <span className={styles.alertCount}>
              {countOf(agent.firingAlerts, 'alert', 'alerts')}
            </span>
          )}
        </span>
        <span className={styles.meta} title={`${agent.hostname} · ${platform}`}>
          {agent.displayName !== '' && <>{agent.hostname} · </>}
          {platform} · {agent.arch}
        </span>
        {agent.tags.length > 0 && (
          <span className={styles.tags}>
            {agent.tags.map((tag) => (
              <span key={tag} className={styles.tag} title={tag}>
                {tag}
              </span>
            ))}
          </span>
        )}
      </div>
      {agent.online && agent.current !== null ? (
        <div className={styles.metrics}>
          <MetricMeter label="CPU" value={values[metricNames.cpu]} />
          <MetricMeter label="RAM" value={values[metricNames.memory]} />
          <MetricMeter label="Disk" value={values[metricNames.disk]} />
          <span className={styles.uptime}>Up {formatUptime(agent.current.uptimeSeconds)}</span>
        </div>
      ) : (
        <div className={styles.offline}>
          {agent.online
            ? 'Waiting for the first reading…'
            : `Offline · last seen ${agent.lastSeenAt === null ? 'never' : formatRelative(agent.lastSeenAt, now)}`}
        </div>
      )}
    </Link>
  );
}
