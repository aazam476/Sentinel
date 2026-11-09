// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { toast } from 'sonner';

import { useAgents } from '../../api/agents';
import { useAlertRules, useChannels, useDeleteAlertRule, useSaveAlertRule } from '../../api/alerts';
import type { AlertRule } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from '../Scripts/Scripts.module.css';
import { describeCondition, describeTarget } from './alertText';
import { RuleDialog } from './RuleDialog';

/** RulesView lists the alert rules and edits them. */
export function RulesView() {
  const rules = useAlertRules();
  const channels = useChannels();
  const agents = useAgents();
  const save = useSaveAlertRule();
  const remove = useDeleteAlertRule();
  const [editing, setEditing] = useState<AlertRule | 'new'>();
  const [deleting, setDeleting] = useState<AlertRule>();
  const machineNames = new Map((agents.data?.agents ?? []).map((agent) => [agent.id, agent.name]));
  const channelNames = new Map(
    (channels.data?.channels ?? []).map((channel) => [channel.id, channel.name]),
  );
  return (
    <section className={styles.section} aria-label="Alert rules">
      <div className={systemStyles.toolbar}>
        <Button
          onClick={() => {
            setEditing('new');
          }}
        >
          New rule
        </Button>
      </div>
      {rules.isPending && <p className={styles.muted}>Loading rules…</p>}
      {rules.isError && <FormError error={rules.error} />}
      {rules.data?.rules.length === 0 && (
        <p className={styles.muted}>There are no rules, so nothing raises alerts.</p>
      )}
      {rules.data !== undefined && rules.data.rules.length > 0 && (
        <ul className={systemStyles.list}>
          {rules.data.rules.map((rule) => (
            <li key={rule.id} className={styles.row} aria-label={`Rule ${rule.name}`}>
              <span className={styles.runHeader}>
                <button
                  type="button"
                  className={systemStyles.linkButton}
                  onClick={() => {
                    setEditing(rule);
                  }}
                >
                  <span className={styles.name}>{rule.name}</span>
                </button>
                {!rule.enabled && <span className={systemStyles.badge}>Paused</span>}
              </span>
              <span className={styles.description}>{describeCondition(rule)}</span>
              <span className={systemStyles.meta}>
                <span>{describeTarget(rule.target, machineNames)}</span>
                <span>
                  {rule.channelIds.length === 0
                    ? 'Notifies nobody'
                    : `Notifies ${rule.channelIds
                        .map((id) => channelNames.get(id) ?? '?')
                        .sort((a, b) => a.localeCompare(b))
                        .join(', ')}`}
                </span>
                <button
                  type="button"
                  className={systemStyles.linkButton}
                  onClick={() => {
                    setDeleting(rule);
                  }}
                >
                  Delete
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
      {editing !== undefined && (
        <RuleDialog
          rule={editing === 'new' ? undefined : editing}
          agents={agents.data?.agents ?? []}
          channels={channels.data?.channels ?? []}
          onClose={() => {
            setEditing(undefined);
          }}
          onSave={(request, done) => {
            save.mutate(request, {
              onSuccess: () => {
                toast.success('Rule saved');
                setEditing(undefined);
              },
              onError: done,
            });
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== undefined}
        onOpenChange={(isOpen) => {
          if (!isOpen) {
            setDeleting(undefined);
          }
        }}
        title={`Delete ${deleting?.name ?? 'this rule'}?`}
        description="Its firing alerts resolve, and it raises no more."
        confirmLabel="Delete"
        onConfirm={() => {
          if (deleting !== undefined) {
            remove.mutate(deleting.id, {
              onSuccess: () => toast.success('Rule deleted'),
              onError: (error) => toast.error(messageFor(error)),
            });
          }
        }}
      />
    </section>
  );
}
