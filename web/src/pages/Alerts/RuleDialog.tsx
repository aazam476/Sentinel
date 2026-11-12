// SPDX-License-Identifier: Apache-2.0
import { Dialog } from '@base-ui/react/dialog';
import { useState } from 'react';

import type {
  Agent,
  AlertRule,
  AlertRuleConfig,
  NotificationChannel,
  SaveAlertRuleRequest,
} from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import confirmStyles from '../../components/ConfirmDialog/ConfirmDialog.module.css';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import dialogStyles from '../../components/StepUpDialog/StepUpDialog.module.css';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from '../Scripts/Scripts.module.css';
import alertStyles from './Alerts.module.css';
import { describeCondition, ruleTypes, watchableMetrics } from './alertText';

interface RuleDialogProps {
  rule?: AlertRule;
  agents: Agent[];
  channels: NotificationChannel[];
  onClose: () => void;
  onSave: (
    request: SaveAlertRuleRequest & { id?: number },
    failed: (error: unknown) => void,
  ) => void;
}

/** defaults are the settings a rule of each type starts with. */
const defaults: Record<string, AlertRuleConfig> = {
  metric: { metric: 'cpu.percent', direction: 'above', threshold: 90, minutes: 5 },
  offline: { minutes: 10 },
  storage: { capacityPercent: 80 },
  script_failed: {},
  container: {},
};

/** RuleDialog adds or changes an alert rule. */
export function RuleDialog({ rule, agents, channels, onClose, onSave }: RuleDialogProps) {
  // Only the fields a save sends: the server refuses any others.
  const [draft, setDraft] = useState<SaveAlertRuleRequest>(
    rule === undefined
      ? {
          type: 'metric',
          name: '',
          config: defaults.metric ?? {},
          target: { kind: 'all', value: '' },
          enabled: true,
          channelIds: channels.map((channel) => channel.id),
        }
      : {
          type: rule.type,
          name: rule.name,
          config: rule.config,
          target: rule.target,
          enabled: rule.enabled,
          channelIds: rule.channelIds,
        },
  );
  const [error, setError] = useState<unknown>();
  const setConfig = (change: Partial<AlertRuleConfig>) => {
    setDraft({ ...draft, config: { ...draft.config, ...change } });
  };
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className={dialogStyles.backdrop} />
        <Dialog.Popup className={dialogStyles.popup}>
          <Dialog.Title className={dialogStyles.title}>
            {rule === undefined ? 'New alert rule' : 'Change alert rule'}
          </Dialog.Title>
          <div className={alertStyles.fields}>
            <Field
              label="Name"
              value={draft.name}
              maxLength={100}
              onChange={(event) => {
                setDraft({ ...draft, name: event.target.value });
              }}
            />
            <label className={styles.setting}>
              <span>Alert when</span>
              <select
                className={systemStyles.select}
                value={draft.type}
                disabled={rule !== undefined}
                onChange={(event) => {
                  const type = event.target.value;
                  setDraft({ ...draft, type, config: defaults[type] ?? {} });
                }}
              >
                {ruleTypes.map((type) => (
                  <option key={type.value} value={type.value}>
                    {type.label}
                  </option>
                ))}
              </select>
            </label>
            <ConditionFields type={draft.type} config={draft.config} onChange={setConfig} />
            <p className={styles.muted}>{describeCondition(draft)}</p>
            <TargetFields
              target={draft.target}
              agents={agents}
              onChange={(target) => {
                setDraft({ ...draft, target });
              }}
            />
            <fieldset className={alertStyles.channelChoices}>
              <legend>Notify</legend>
              {channels.length === 0 && (
                <p className={styles.muted}>
                  No channels yet: the alert shows here, but nobody is told.
                </p>
              )}
              {channels.map((channel) => (
                <label key={channel.id} className={styles.choice}>
                  <input
                    type="checkbox"
                    checked={draft.channelIds.includes(channel.id)}
                    onChange={(event) => {
                      setDraft({
                        ...draft,
                        channelIds: event.target.checked
                          ? [...draft.channelIds, channel.id]
                          : draft.channelIds.filter((id) => id !== channel.id),
                      });
                    }}
                  />
                  <span className={styles.machineName}>{channel.name}</span>
                </label>
              ))}
            </fieldset>
            <label className={styles.choice}>
              <input
                type="checkbox"
                checked={draft.enabled}
                onChange={(event) => {
                  setDraft({ ...draft, enabled: event.target.checked });
                }}
              />
              On (unticked, the rule is paused and its alerts resolve)
            </label>
          </div>
          <FormError error={error} />
          <div className={confirmStyles.actions}>
            <Dialog.Close className={dialogStyles.close}>Cancel</Dialog.Close>
            <Button
              disabled={draft.name.trim() === ''}
              onClick={() => {
                setError(undefined);
                onSave({ ...draft, id: rule?.id }, setError);
              }}
            >
              Save
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

interface ConditionFieldsProps {
  type: string;
  config: AlertRuleConfig;
  onChange: (change: Partial<AlertRuleConfig>) => void;
}

function numberOf(text: string): number {
  return text === '' ? 0 : Number(text);
}

/** ConditionFields are the settings the rule's type uses. */
function ConditionFields({ type, config, onChange }: ConditionFieldsProps) {
  if (type === 'metric') {
    return (
      <div className={alertStyles.inline}>
        <label className={styles.setting}>
          <span>Metric</span>
          <select
            className={systemStyles.select}
            value={config.metric}
            onChange={(event) => {
              onChange({ metric: event.target.value });
            }}
          >
            {watchableMetrics.map((metric) => (
              <option key={metric.value} value={metric.value}>
                {metric.label}
              </option>
            ))}
          </select>
        </label>
        <label className={styles.setting}>
          <span>Is</span>
          <select
            className={systemStyles.select}
            value={config.direction}
            onChange={(event) => {
              onChange({ direction: event.target.value });
            }}
          >
            <option value="above">above</option>
            <option value="below">below</option>
          </select>
        </label>
        <Field
          label="Threshold"
          type="number"
          inputMode="decimal"
          min={0}
          max={1000}
          value={config.threshold ?? 0}
          onChange={(event) => {
            onChange({ threshold: numberOf(event.target.value) });
          }}
        />
        <Field
          label="For minutes"
          type="number"
          inputMode="numeric"
          min={1}
          max={1440}
          value={config.minutes ?? 0}
          onChange={(event) => {
            onChange({ minutes: numberOf(event.target.value) });
          }}
        />
      </div>
    );
  }
  if (type === 'offline') {
    return (
      <Field
        label="Offline for minutes"
        type="number"
        inputMode="numeric"
        min={1}
        max={10080}
        value={config.minutes ?? 0}
        onChange={(event) => {
          onChange({ minutes: numberOf(event.target.value) });
        }}
      />
    );
  }
  if (type === 'storage') {
    return (
      <Field
        label="Full above (%)"
        type="number"
        inputMode="numeric"
        min={1}
        max={100}
        hint="Pools, and datasets against their quota. Pool health, device errors and failing disks always alert."
        value={config.capacityPercent ?? 80}
        onChange={(event) => {
          onChange({ capacityPercent: numberOf(event.target.value) });
        }}
      />
    );
  }
  return null;
}

interface TargetFieldsProps {
  target: SaveAlertRuleRequest['target'];
  agents: Agent[];
  onChange: (target: SaveAlertRuleRequest['target']) => void;
}

/** TargetFields choose which machines the rule watches. */
function TargetFields({ target, agents, onChange }: TargetFieldsProps) {
  return (
    <div className={alertStyles.inline}>
      <label className={styles.setting}>
        <span>On</span>
        <select
          className={systemStyles.select}
          value={target.kind}
          onChange={(event) => {
            const kind = event.target.value;
            onChange({ kind, value: kind === 'agent' ? (agents[0]?.id ?? '') : '' });
          }}
        >
          <option value="all">All machines</option>
          <option value="tag">Machines with a tag</option>
          <option value="agent">One machine</option>
        </select>
      </label>
      {target.kind === 'tag' && (
        <Field
          label="Tag"
          value={target.value}
          autoCapitalize="none"
          autoCorrect="off"
          onChange={(event) => {
            onChange({ kind: 'tag', value: event.target.value });
          }}
        />
      )}
      {target.kind === 'agent' && (
        <label className={styles.setting}>
          <span>Machine</span>
          <select
            className={systemStyles.select}
            value={target.value}
            onChange={(event) => {
              onChange({ kind: 'agent', value: event.target.value });
            }}
          >
            {agents.map((agent) => (
              <option key={agent.id} value={agent.id}>
                {agent.name}
              </option>
            ))}
          </select>
        </label>
      )}
    </div>
  );
}
