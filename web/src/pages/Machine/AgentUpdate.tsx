// SPDX-License-Identifier: Apache-2.0
import { toast } from 'sonner';

import type { AgentDetails } from '../../api/types.gen';
import { useAgentBuilds, useUpdateAgents } from '../../api/updates';
import { Button } from '../../components/Button/Button';
import { FormError } from '../../components/FormError/FormError';
import { formatDateTime } from '../../formatting/formatting';
import alertStyles from '../Alerts/Alerts.module.css';
import styles from './MachinePage.module.css';

const stateText: Record<string, string> = {
  updating: 'Updating to',
  updated: 'Updated to',
  failed: 'Couldn’t update to',
  rolled_back: 'Went back after trying',
};

/**
 * AgentUpdate shows the machine's agent version and last update, and lets
 * an admin update it to the newest build for its system.
 */
export function AgentUpdate({ agent }: { agent: AgentDetails }) {
  const builds = useAgentBuilds();
  const update = useUpdateAgents();
  const newest = builds.data?.builds.find(
    (build) => build.os === agent.os && build.arch === agent.arch,
  );
  const last = agent.update;
  return (
    <section className={styles.panel} aria-labelledby="agent-update-title">
      <h2 id="agent-update-title" className={styles.panelTitle}>
        Agent
      </h2>
      <p className={styles.muted}>Running {agent.agentVersion}.</p>
      {last !== null && (
        <p
          className={
            last.state === 'failed' || last.state === 'rolled_back'
              ? alertStyles.problem
              : styles.muted
          }
        >
          {stateText[last.state] ?? last.state} {last.version}
          {last.finishedAt !== null && ` · ${formatDateTime(last.finishedAt)}`}
          {last.error !== '' && `: ${last.error}`}
        </p>
      )}
      {newest !== undefined && newest.version !== agent.agentVersion && (
        <div className={styles.settingsForm}>
          <Button
            variant="secondary"
            disabled={!agent.online || last?.state === 'updating'}
            isBusy={update.isPending}
            onClick={() => {
              update.mutate(
                { target: { kind: 'agent', value: agent.id }, version: newest.version },
                { onSuccess: () => toast.success(`Updating to ${newest.version}`) },
              );
            }}
          >
            Update to {newest.version}
          </Button>
        </div>
      )}
      <FormError error={update.error} />
    </section>
  );
}
