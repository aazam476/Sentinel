// SPDX-License-Identifier: Apache-2.0
import { type SubmitEvent, useState } from 'react';
import { useOutletContext } from 'react-router';
import { toast } from 'sonner';

import type { SessionResponse } from '../../api/types.gen';
import {
  useAgentBuilds,
  useFetchBuilds,
  useSetAutoUpdate,
  useUpdateAgents,
  useUploadBuild,
} from '../../api/updates';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import { countOf, formatBytes, formatDateTime } from '../../formatting/formatting';
import alertStyles from '../Alerts/Alerts.module.css';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from '../Scripts/Scripts.module.css';
import updateStyles from './Updates.module.css';

/**
 * UpdatesPage hosts agent builds and updates machines with them (§11.3):
 * admins upload a signed build or fetch the latest release, then update
 * every machine or let them update by themselves.
 */
export function UpdatesPage() {
  const session = useOutletContext<SessionResponse>();
  const builds = useAgentBuilds();
  const fetchBuilds = useFetchBuilds();
  const setAuto = useSetAutoUpdate();
  const updateAll = useUpdateAgents();
  const [confirming, setConfirming] = useState(false);

  if (session.user.role !== 'admin') {
    return (
      <div className={styles.page}>
        <h1 className={styles.title}>Agent updates</h1>
        <p className={styles.muted}>Only admins update agents.</p>
      </div>
    );
  }
  const data = builds.data;
  return (
    <div className={styles.page}>
      <h1 className={styles.title}>Agent updates</h1>
      {data?.developmentKey === true && (
        <p className={alertStyles.problem} role="note">
          This server trusts only the development release key, so it accepts builds anyone can sign.
          Use it for testing only.
        </p>
      )}
      <section className={styles.section} aria-label="Update settings">
        <label className={styles.choice}>
          <input
            type="checkbox"
            checked={data?.autoUpdate ?? false}
            disabled={data === undefined || setAuto.isPending}
            onChange={(event) => {
              const enabled = event.target.checked;
              setAuto.mutate(enabled, {
                onSuccess: () =>
                  toast.success(
                    enabled ? 'Machines will update by themselves' : 'Automatic updates are off',
                  ),
                onError: (error) => toast.error(messageFor(error)),
              });
            }}
          />
          Update machines automatically to the newest build (checked every hour)
        </label>
        <div className={alertStyles.actions}>
          <Button
            disabled={data === undefined || data.builds.length === 0}
            onClick={() => {
              setConfirming(true);
            }}
          >
            Update all machines
          </Button>
          {data?.canFetch === true && (
            <Button
              variant="secondary"
              isBusy={fetchBuilds.isPending}
              onClick={() => {
                fetchBuilds.mutate(undefined, {
                  onSuccess: (fetched) =>
                    toast.success(`Fetched ${countOf(fetched.builds.length, 'build', 'builds')}`),
                  onError: (error) => toast.error(messageFor(error)),
                });
              }}
            >
              Fetch the latest release
            </Button>
          )}
        </div>
      </section>
      <UploadForm />
      <section className={styles.section} aria-label="Agent builds">
        <h2 className={alertStyles.heading}>Builds</h2>
        {builds.isPending && <p className={styles.muted}>Loading builds…</p>}
        <FormError error={builds.error} />
        {data?.builds.length === 0 && (
          <p className={styles.muted}>No builds yet. Upload one, or fetch the latest release.</p>
        )}
        {data !== undefined && data.builds.length > 0 && (
          <ul className={systemStyles.list}>
            {data.builds.map((build) => (
              <li key={build.file} className={styles.row} aria-label={build.file}>
                <span className={styles.runHeader}>
                  <span className={styles.name}>{build.version}</span>
                  <span className={systemStyles.badge}>
                    {build.os}/{build.arch}
                  </span>
                </span>
                <span className={systemStyles.meta}>
                  <span className={updateStyles.fileName}>{build.file}</span>
                  <span>{formatBytes(build.size)}</span>
                  <span>Added {formatDateTime(build.addedAt)}</span>
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Update every machine?"
        description="Each online machine with a build for its system updates to the newest one. A machine whose new version can't connect goes back to the old one by itself."
        confirmLabel="Update"
        onConfirm={() => {
          updateAll.mutate(
            { target: { kind: 'all', value: '' }, version: '' },
            {
              onSuccess: (outcome) => {
                const skipped = Object.keys(outcome.skipped).length;
                toast.success(
                  `${countOf(outcome.started.length, 'machine is', 'machines are')} updating` +
                    (skipped > 0 ? `; ${countOf(skipped, 'was', 'were')} skipped` : ''),
                );
              },
              onError: (error) => toast.error(messageFor(error)),
            },
          );
        }}
      />
    </div>
  );
}

/** UploadForm hosts a build and its .sig file, for servers without internet. */
function UploadForm() {
  const upload = useUploadBuild();
  const [build, setBuild] = useState<File>();
  const [signature, setSignature] = useState<File>();
  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = event.currentTarget;
    if (build === undefined || signature === undefined) {
      return;
    }
    upload.mutate(
      { build, signature },
      {
        onSuccess: (added) => {
          toast.success(`Added ${added.version} for ${added.os}/${added.arch}`);
          form.reset();
          setBuild(undefined);
          setSignature(undefined);
        },
      },
    );
  };
  return (
    <section className={styles.section} aria-label="Upload a build">
      <h2 className={alertStyles.heading}>Upload a build</h2>
      <p className={styles.muted}>
        For servers that can’t reach the internet: the agent binary and its .sig file from a
        release. The signature is checked before the build is kept.
      </p>
      <form className={alertStyles.inline} onSubmit={submit}>
        <label className={styles.setting}>
          <span>Agent binary</span>
          <input
            type="file"
            className={updateStyles.picker}
            onChange={(event) => {
              setBuild(event.target.files?.[0]);
            }}
          />
        </label>
        <label className={styles.setting}>
          <span>Signature (.sig)</span>
          <input
            type="file"
            className={updateStyles.picker}
            accept=".sig"
            onChange={(event) => {
              setSignature(event.target.files?.[0]);
            }}
          />
        </label>
        <Button
          type="submit"
          variant="secondary"
          isBusy={upload.isPending}
          disabled={build === undefined || signature === undefined}
        >
          Upload
        </Button>
      </form>
      <FormError error={upload.error} />
    </section>
  );
}
