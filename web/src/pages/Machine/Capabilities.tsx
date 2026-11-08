// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';

import type { Capability } from '../../api/types.gen';
import styles from './MachinePage.module.css';

/** capabilityLabels names each capability the way the UI talks about it. */
const capabilityLabels: Record<string, string> = {
  terminal: 'Terminal',
  desktop: 'Remote desktop',
  files: 'Files',
  scripts: 'Scripts',
  docker: 'Docker',
  storage: 'Storage',
  packages: 'Package updates',
  power: 'Restart and shut down',
};

/**
 * Capabilities shows which features the machine's local policy file
 * allows. The server can't change that file (SEC-22), so a disabled one
 * says where the setting lives.
 */
export function Capabilities({ capabilities }: { capabilities: Capability[] }) {
  return (
    <section className={styles.panel} aria-labelledby="capabilities-title">
      <h2 id="capabilities-title" className={styles.panelTitle}>
        Capabilities
      </h2>
      {capabilities.length === 0 ? (
        <p className={styles.muted}>Not reported yet.</p>
      ) : (
        <ul className={styles.capabilities}>
          {capabilities.map((capability) => (
            <li key={capability.name} className={styles.capability}>
              <span>{capabilityLabels[capability.name] ?? capability.name}</span>
              <span
                className={clsx(styles.capabilityState, !capability.enabled && styles.disabled)}
              >
                {capability.enabled ? 'Allowed' : capability.reason || 'Disabled'}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
