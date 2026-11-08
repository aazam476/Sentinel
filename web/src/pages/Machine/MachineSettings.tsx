// SPDX-License-Identifier: Apache-2.0
import { type SubmitEvent, useState } from 'react';
import { useNavigate } from 'react-router';
import { toast } from 'sonner';

import { useRevokeAgent, useUpdateAgent } from '../../api/agents';
import type { AgentDetails } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import styles from './MachinePage.module.css';

/** MachineSettings lets an admin rename, tag or revoke a machine. */
export function MachineSettings({ agent }: { agent: AgentDetails }) {
  const update = useUpdateAgent(agent.id);
  const revoke = useRevokeAgent(agent.id);
  const navigate = useNavigate();
  const [displayName, setDisplayName] = useState(agent.displayName);
  const [tags, setTags] = useState(agent.tags.join(', '));
  const [isConfirming, setIsConfirming] = useState(false);

  const handleSave = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    const tagList = tags.split(/[\s,]+/).filter((tag) => tag !== '');
    update.mutate({ displayName, tags: tagList }, { onSuccess: () => toast.success('Saved') });
  };

  const handleRevoke = () => {
    revoke.mutate(undefined, {
      onSuccess: () => {
        toast.success(`Revoked ${agent.name}`);
        void navigate('/');
      },
    });
  };

  return (
    <section className={styles.panel} aria-labelledby="settings-title">
      <h2 id="settings-title" className={styles.panelTitle}>
        Settings
      </h2>
      <form className={styles.settingsForm} onSubmit={handleSave}>
        <label className={styles.field}>
          <span className={styles.label}>Display name</span>
          <input
            className={styles.input}
            value={displayName}
            maxLength={64}
            placeholder={agent.hostname}
            onChange={(event) => {
              setDisplayName(event.target.value);
            }}
          />
        </label>
        <label className={styles.field}>
          <span className={styles.label}>Tags, separated by commas</span>
          <input
            className={styles.input}
            value={tags}
            autoCapitalize="none"
            autoCorrect="off"
            placeholder="homelab, storage"
            onChange={(event) => {
              setTags(event.target.value);
            }}
          />
        </label>
        <Button type="submit" variant="secondary" isBusy={update.isPending}>
          Save
        </Button>
      </form>
      <FormError error={update.error} />
      <div className={styles.danger}>
        <p className={styles.muted}>
          Revoking disconnects the machine at once and it can never connect again with its current
          identity. To bring it back, add it again.
        </p>
        <Button
          variant="danger"
          isBusy={revoke.isPending}
          onClick={() => {
            setIsConfirming(true);
          }}
        >
          Revoke machine
        </Button>
      </div>
      <FormError error={revoke.error} />
      <ConfirmDialog
        open={isConfirming}
        onOpenChange={setIsConfirming}
        title={`Revoke ${agent.name}?`}
        description="It disconnects now and can’t reconnect until it is added again."
        confirmLabel="Revoke"
        onConfirm={handleRevoke}
      />
    </section>
  );
}
