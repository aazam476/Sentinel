// SPDX-License-Identifier: Apache-2.0
import type { AgentDetails } from '../../api/types.gen';
import { formatBytes, formatDateTime, formatUptime } from '../../formatting/formatting';
import styles from './MachinePage.module.css';

/** SystemDetails is what the agent reported about its machine. */
export function SystemDetails({ agent }: { agent: AgentDetails }) {
  const rows: [string, string][] = [
    ['Hostname', agent.hostname],
    [
      'System',
      [agent.platform || agent.os, agent.platformVersion].filter(Boolean).join(' ') || '–',
    ],
    ['Kernel', agent.kernelVersion || '–'],
    ['Architecture', agent.arch],
    [
      'Processor',
      agent.cpuModel === '' ? '–' : `${agent.cpuModel} (${String(agent.cpuCores)} cores)`,
    ],
    ['Memory', agent.memoryTotalBytes > 0 ? formatBytes(agent.memoryTotalBytes) : '–'],
    ['Uptime', agent.current === null ? '–' : formatUptime(agent.current.uptimeSeconds)],
    [
      'Network',
      agent.interfaces
        .filter((iface) => iface.addresses.length > 0)
        .map(
          (iface) =>
            `${iface.name} ${iface.addresses.join(', ')}${iface.mac === '' ? '' : ` (${iface.mac})`}`,
        )
        .join(' · ') || '–',
    ],
    ['Agent version', agent.agentVersion],
    ['Enrolled', formatDateTime(agent.enrolledAt)],
  ];
  return (
    <section className={styles.panel} aria-labelledby="system-title">
      <h2 id="system-title" className={styles.panelTitle}>
        System
      </h2>
      <dl className={styles.details}>
        {rows.map(([term, value]) => (
          <div key={term} className={styles.detail}>
            <dt>{term}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
