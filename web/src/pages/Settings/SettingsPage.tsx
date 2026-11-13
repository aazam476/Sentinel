// SPDX-License-Identifier: Apache-2.0
import { useState, type SubmitEvent } from 'react';
import { Link, useOutletContext } from 'react-router';
import { toast } from 'sonner';

import { isApiError } from '../../api/client';
import { useSaveSettings, useSettings } from '../../api/settings';
import type { ServerConfig, SessionResponse, SettingsResponse } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import { StepUpDialog } from '../../components/StepUpDialog/StepUpDialog';
import { useStepUpRetry } from '../../components/StepUpDialog/useStepUpRetry';
import { formatBytes } from '../../formatting/formatting';
import machineStyles from '../Machine/MachinePage.module.css';
import styles from '../Scripts/Scripts.module.css';

/**
 * SettingsPage is where admins change the server's settings (§9.10): how
 * long metrics history is kept and the release channel. What server.toml
 * sets is shown read-only, with where the rest of the settings live.
 */
export function SettingsPage() {
  const session = useOutletContext<SessionResponse>();
  const settings = useSettings();

  if (session.user.role !== 'admin') {
    return (
      <div className={styles.page}>
        <h1 className={styles.title}>Settings</h1>
        <p className={styles.muted}>Only admins change the server’s settings.</p>
      </div>
    );
  }
  return (
    <div className={styles.page}>
      <h1 className={styles.title}>Settings</h1>
      {settings.isPending && <p className={styles.muted}>Loading…</p>}
      <FormError error={settings.error} />
      {settings.data !== undefined && (
        <>
          <SettingsForm
            key={JSON.stringify(settings.data)}
            current={settings.data}
            session={session}
          />
          <section className={styles.section} aria-labelledby="elsewhere-title">
            <h2 id="elsewhere-title" className={styles.subtitle}>
              Elsewhere
            </h2>
            <p className={styles.muted}>
              Email (SMTP) and the other notification channels are on{' '}
              <Link to="/alerts/channels">Alerts → Channels</Link>. Whether everyone must use
              two-factor authentication is on <Link to="/users">Users</Link>.
            </p>
          </section>
          <ServerFile server={settings.data.server} />
        </>
      )}
    </div>
  );
}

interface SettingsFormProps {
  current: SettingsResponse;
  session: SessionResponse;
}

/** SettingsForm edits retention and the release channel. */
function SettingsForm({ current, session }: SettingsFormProps) {
  const save = useSaveSettings();
  const stepUp = useStepUpRetry();
  const [rawHours, setRawHours] = useState(String(current.retention.rawHours));
  const [minuteDays, setMinuteDays] = useState(String(current.retention.minuteDays));
  const [hourDays, setHourDays] = useState(String(current.retention.hourDays));
  const [channel, setChannel] = useState(current.updateChannel);

  const submit = () => {
    save.mutate(
      {
        retention: {
          rawHours: Number(rawHours),
          minuteDays: Number(minuteDays),
          hourDays: Number(hourDays),
        },
        updateChannel: channel,
      },
      {
        onSuccess: () => toast.success('Settings saved'),
        onError: (error) => stepUp.handleError(error, submit),
      },
    );
  };
  const handleSubmit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    submit();
  };
  const fieldError = (field: string) =>
    isApiError(save.error, 'invalid_input') && save.error.field === field
      ? save.error.message
      : undefined;
  const otherError =
    isApiError(save.error, 'step_up_required') ||
    (isApiError(save.error, 'invalid_input') &&
      ['rawHours', 'minuteDays', 'hourDays', 'updateChannel'].includes(save.error.field ?? ''))
      ? null
      : save.error;

  return (
    <form
      className={styles.section}
      onSubmit={handleSubmit}
      aria-labelledby="history-title"
      noValidate
    >
      <h2 id="history-title" className={styles.subtitle}>
        Metrics history
      </h2>
      <p className={styles.muted}>
        Older detail is averaged away: raw samples, then minutes, then hours.
      </p>
      <div className={styles.settings}>
        <Field
          label="Raw samples (hours)"
          type="number"
          min={1}
          max={168}
          value={rawHours}
          error={fieldError('rawHours')}
          onChange={(event) => {
            setRawHours(event.target.value);
          }}
        />
        <Field
          label="Minute averages (days)"
          type="number"
          min={1}
          max={90}
          value={minuteDays}
          error={fieldError('minuteDays')}
          onChange={(event) => {
            setMinuteDays(event.target.value);
          }}
        />
        <Field
          label="Hourly averages (days)"
          type="number"
          min={30}
          max={1825}
          value={hourDays}
          error={fieldError('hourDays')}
          onChange={(event) => {
            setHourDays(event.target.value);
          }}
        />
      </div>
      <h2 className={styles.subtitle}>Updates</h2>
      <label className={styles.setting}>
        <span>Release channel</span>
        <select
          value={channel}
          onChange={(event) => {
            setChannel(event.target.value);
          }}
        >
          <option value="stable">Stable releases only</option>
          <option value="beta">Beta releases too</option>
        </select>
      </label>
      <p className={styles.muted}>
        Where “Fetch the latest release” on Updates gets agent builds from.
      </p>
      {fieldError('updateChannel') !== undefined && (
        <p role="alert">{fieldError('updateChannel')}</p>
      )}
      <FormError error={otherError} />
      <div>
        <Button type="submit" isBusy={save.isPending}>
          Save
        </Button>
      </div>
      <StepUpDialog
        open={stepUp.isOpen}
        onOpenChange={stepUp.setOpen}
        secondFactors={session.secondFactors}
        onConfirmed={stepUp.confirmed}
      />
    </form>
  );
}

/** ServerFile shows server.toml's settings, which need a restart to change. */
function ServerFile({ server }: { server: ServerConfig }) {
  const rows: [string, string][] = [
    ['Listen address', server.listen],
    ['Public URL', server.publicUrl || 'Not set'],
    ['Base path', server.basePath],
    ['Trusted proxies', server.trustedProxies.join(', ') || 'None'],
    ['TLS', server.tlsMode + (server.behindTlsProxy ? ', behind a TLS proxy' : '')],
    ['ACME email', server.acmeEmail || 'Not set'],
    ['Release channel address', server.releaseUrl || 'Off: upload builds by hand'],
    ['Largest file', formatBytes(server.maxFileBytes)],
    ['Remote desktop relay', server.turnListen || 'Off'],
  ];
  return (
    <section className={styles.section} aria-labelledby="server-file-title">
      <h2 id="server-file-title" className={styles.subtitle}>
        Server (server.toml)
      </h2>
      <p className={styles.muted}>
        These decide how the server is reached, so a mistake could lock everyone out. Change them in
        server.toml and restart the server.
      </p>
      <dl className={machineStyles.details}>
        {rows.map(([term, value]) => (
          <div key={term} className={machineStyles.detail}>
            <dt>{term}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
