// SPDX-License-Identifier: Apache-2.0
import { Dialog } from '@base-ui/react/dialog';
import { useState } from 'react';

import type {
  NotificationChannel,
  NotificationChannelConfig,
  SaveNotificationChannelRequest,
} from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import confirmStyles from '../../components/ConfirmDialog/ConfirmDialog.module.css';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import dialogStyles from '../../components/StepUpDialog/StepUpDialog.module.css';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from '../Scripts/Scripts.module.css';
import alertStyles from './Alerts.module.css';
import { channelTypes } from './alertText';

interface ChannelDialogProps {
  channel?: NotificationChannel;
  onClose: () => void;
  onSave: (
    request: SaveNotificationChannelRequest & { id?: number },
    failed: (error: unknown) => void,
  ) => void;
}

/**
 * ChannelDialog adds or changes a notification channel. Secrets (the SMTP
 * password, the ntfy token, webhook addresses) are never shown again;
 * left empty in a change, the stored ones are kept.
 */
export function ChannelDialog({ channel, onClose, onSave }: ChannelDialogProps) {
  const [draft, setDraft] = useState<SaveNotificationChannelRequest>(
    channel === undefined
      ? { type: 'email', name: '', enabled: true, config: {} }
      : {
          type: channel.type,
          name: channel.name,
          enabled: channel.enabled,
          config: channel.config,
        },
  );
  const [recipients, setRecipients] = useState((channel?.config.to ?? []).join(', '));
  const [error, setError] = useState<unknown>();
  const keeps = channel?.hasSecret === true ? 'Leave empty to keep the one stored.' : undefined;
  const setConfig = (change: Partial<NotificationChannelConfig>) => {
    setDraft({ ...draft, config: { ...draft.config, ...change } });
  };
  const save = () => {
    setError(undefined);
    const config =
      draft.type === 'email'
        ? { ...draft.config, to: recipients.split(/[\s,;]+/).filter((to) => to !== '') }
        : draft.config;
    onSave({ ...draft, config, id: channel?.id }, setError);
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
            {channel === undefined ? 'New channel' : 'Change channel'}
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
              <span>Type</span>
              <select
                className={systemStyles.select}
                value={draft.type}
                disabled={channel !== undefined}
                onChange={(event) => {
                  setDraft({ ...draft, type: event.target.value, config: {} });
                }}
              >
                {channelTypes.map((type) => (
                  <option key={type.value} value={type.value}>
                    {type.label}
                  </option>
                ))}
              </select>
            </label>
            {draft.type === 'email' && (
              <EmailFields
                config={draft.config}
                recipients={recipients}
                passwordHint={keeps}
                onChange={setConfig}
                onRecipients={setRecipients}
              />
            )}
            {(draft.type === 'webhook' || draft.type === 'discord') && (
              <Field
                label={draft.type === 'discord' ? 'Discord webhook URL' : 'URL'}
                type="url"
                inputMode="url"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                value={draft.config.url ?? ''}
                hint={
                  keeps ??
                  (draft.type === 'webhook'
                    ? 'Each alert is posted here as JSON.'
                    : 'From the channel’s settings, under Integrations.')
                }
                onChange={(event) => {
                  setConfig({ url: event.target.value });
                }}
              />
            )}
            {draft.type === 'ntfy' && (
              <>
                <Field
                  label="Server"
                  type="url"
                  inputMode="url"
                  autoCapitalize="none"
                  autoCorrect="off"
                  placeholder="https://ntfy.sh"
                  value={draft.config.server ?? ''}
                  onChange={(event) => {
                    setConfig({ server: event.target.value });
                  }}
                />
                <Field
                  label="Topic"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  value={draft.config.topic ?? ''}
                  onChange={(event) => {
                    setConfig({ topic: event.target.value });
                  }}
                />
                <Field
                  label="Access token (optional)"
                  type="password"
                  autoComplete="off"
                  value={draft.config.token ?? ''}
                  hint={keeps}
                  onChange={(event) => {
                    setConfig({ token: event.target.value });
                  }}
                />
              </>
            )}
            <label className={styles.choice}>
              <input
                type="checkbox"
                checked={draft.enabled}
                onChange={(event) => {
                  setDraft({ ...draft, enabled: event.target.checked });
                }}
              />
              On (unticked, nothing is sent through it)
            </label>
          </div>
          <FormError error={error} />
          <div className={confirmStyles.actions}>
            <Dialog.Close className={dialogStyles.close}>Cancel</Dialog.Close>
            <Button disabled={draft.name.trim() === ''} onClick={save}>
              Save
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

interface EmailFieldsProps {
  config: NotificationChannelConfig;
  recipients: string;
  passwordHint?: string;
  onChange: (change: Partial<NotificationChannelConfig>) => void;
  onRecipients: (recipients: string) => void;
}

function EmailFields({
  config,
  recipients,
  passwordHint,
  onChange,
  onRecipients,
}: EmailFieldsProps) {
  return (
    <>
      <div className={alertStyles.inline}>
        <Field
          label="Mail server"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          placeholder="smtp.example.com"
          value={config.smtpHost ?? ''}
          onChange={(event) => {
            onChange({ smtpHost: event.target.value });
          }}
        />
        <label className={styles.setting}>
          <span>Security</span>
          <select
            className={systemStyles.select}
            value={config.smtpSecurity ?? 'starttls'}
            onChange={(event) => {
              onChange({ smtpSecurity: event.target.value, smtpPort: undefined });
            }}
          >
            <option value="starttls">STARTTLS (port 587)</option>
            <option value="tls">TLS (port 465)</option>
            <option value="none">None (a relay on this network)</option>
          </select>
        </label>
        <Field
          label="Port"
          type="number"
          inputMode="numeric"
          min={1}
          max={65535}
          placeholder="Default"
          value={config.smtpPort ?? ''}
          onChange={(event) => {
            onChange({
              smtpPort: event.target.value === '' ? undefined : Number(event.target.value),
            });
          }}
        />
      </div>
      <div className={alertStyles.inline}>
        <Field
          label="Username (optional)"
          autoCapitalize="none"
          autoCorrect="off"
          autoComplete="off"
          value={config.smtpUsername ?? ''}
          onChange={(event) => {
            onChange({ smtpUsername: event.target.value });
          }}
        />
        <Field
          label="Password"
          type="password"
          autoComplete="new-password"
          value={config.smtpPassword ?? ''}
          hint={passwordHint}
          onChange={(event) => {
            onChange({ smtpPassword: event.target.value });
          }}
        />
      </div>
      <Field
        label="From"
        type="email"
        inputMode="email"
        autoCapitalize="none"
        placeholder="sentinel@example.com"
        value={config.from ?? ''}
        onChange={(event) => {
          onChange({ from: event.target.value });
        }}
      />
      <Field
        label="To"
        inputMode="email"
        autoCapitalize="none"
        autoCorrect="off"
        hint="Addresses separated by commas."
        value={recipients}
        onChange={(event) => {
          onRecipients(event.target.value);
        }}
      />
    </>
  );
}
