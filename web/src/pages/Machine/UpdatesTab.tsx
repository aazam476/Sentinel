// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { useState } from 'react';
import { toast } from 'sonner';

import { useInstallUpdates, usePackageUpdates } from '../../api/packages';
import type { ApplyPackagesResponse, PackageSource } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import { StepUpDialog } from '../../components/StepUpDialog/StepUpDialog';
import { useStepUpRetry } from '../../components/StepUpDialog/useStepUpRetry';
import { countOf } from '../../formatting/formatting';
import { useMachine } from './machineContext';
import styles from './SystemTab.module.css';
import updateStyles from './UpdatesTab.module.css';

const managerLabels: Record<PackageSource['manager'], string> = {
  apt: 'APT',
  dnf: 'DNF',
  zypper: 'Zypper',
  pacman: 'pacman',
  apk: 'apk',
  softwareupdate: 'Apple software updates',
  homebrew: 'Homebrew',
  'windows-update': 'Windows Update',
  winget: 'winget',
};

/** UpdatesTab lists a machine's waiting operating system updates (§9.6) and installs them. */
export function UpdatesTab() {
  const { machine } = useMachine();
  const updates = usePackageUpdates(machine.id);

  if (!machine.online) {
    return <p className={styles.muted}>The machine is offline. Its updates show when it’s back.</p>;
  }
  if (updates.isPending) {
    return (
      <p className={styles.muted} role="status">
        Checking for updates… The machine refreshes its package lists first, which can take a
        minute.
      </p>
    );
  }
  if (updates.data === undefined) {
    return <FormError error={updates.error} />;
  }
  return (
    <section className={styles.tab} aria-label="Updates">
      <div className={styles.toolbar}>
        <Button
          variant="secondary"
          isBusy={updates.isFetching}
          onClick={() => {
            void updates.refetch();
          }}
        >
          Check again
        </Button>
      </div>
      {updates.data.restartRequired && (
        <p className={updateStyles.notice}>
          Installed updates are waiting for the machine to restart.
        </p>
      )}
      {updates.data.sources.map((source) => (
        <Source key={source.manager} source={source} />
      ))}
    </section>
  );
}

/** Source is one package manager's updates, with what can be installed. */
function Source({ source }: { source: PackageSource }) {
  const { machine, session } = useMachine();
  const install = useInstallUpdates(machine.id);
  const stepUp = useStepUpRetry();
  const [chosen, setChosen] = useState<string[]>([]);
  const [confirmAll, setConfirmAll] = useState(false);
  const [result, setResult] = useState<ApplyPackagesResponse>();
  const label = managerLabels[source.manager];
  const titleId = `updates-${source.manager}`;

  const run = (ids: string[]) => {
    install.mutate(
      { manager: source.manager, ids },
      {
        onSuccess: (done) => {
          setResult(done);
          setChosen([]);
          toast.success(`${label}: updates installed`);
        },
        onError: (error) => {
          if (
            !stepUp.handleError(error, () => {
              run(ids);
            })
          ) {
            toast.error(messageFor(error));
          }
        },
      },
    );
  };
  const toggle = (id: string, on: boolean) => {
    setChosen((current) => (on ? [...current, id] : current.filter((other) => other !== id)));
  };

  return (
    <section className={updateStyles.source} aria-labelledby={titleId}>
      <div className={updateStyles.sourceHeader}>
        <h2 id={titleId} className={updateStyles.sourceTitle}>
          {label}
        </h2>
        {source.updates.length > 0 && (
          <div className={updateStyles.actions}>
            {source.selectable && chosen.length > 0 && (
              <Button
                isBusy={install.isPending}
                onClick={() => {
                  run(chosen);
                }}
              >
                Install {countOf(chosen.length, 'update', 'updates')}
              </Button>
            )}
            <Button
              variant={chosen.length > 0 ? 'secondary' : 'primary'}
              isBusy={install.isPending}
              onClick={() => {
                setConfirmAll(true);
              }}
            >
              Install all
            </Button>
          </div>
        )}
      </div>
      {source.error !== '' ? (
        <p className={styles.muted} role="alert">
          {label} couldn’t be checked: {source.error}
        </p>
      ) : (
        <p className={styles.count}>
          {source.updates.length === 0
            ? 'Up to date.'
            : `${countOf(source.updates.length, 'update', 'updates')} available${securityText(source)}.`}
          {!source.selectable &&
            source.updates.length > 0 &&
            ' This manager installs them all at once.'}
        </p>
      )}
      {install.isPending && (
        <p className={styles.muted} role="status">
          Installing… This can take a while. It carries on if you leave this page.
        </p>
      )}
      {source.updates.length > 0 && (
        <ul className={styles.list} aria-label={`${label} updates`}>
          {source.updates.map((update) => (
            <li key={update.id} className={updateStyles.update}>
              {source.selectable ? (
                <input
                  type="checkbox"
                  aria-label={`Choose ${update.name}`}
                  checked={chosen.includes(update.id)}
                  onChange={(event) => {
                    toggle(update.id, event.target.checked);
                  }}
                />
              ) : (
                <span />
              )}
              <span className={updateStyles.updateText}>
                <span className={styles.name}>{update.name}</span>
                <span className={styles.meta}>
                  {versionText(update.current, update.available) !== '' && (
                    <span>{versionText(update.current, update.available)}</span>
                  )}
                  {update.security && (
                    <span className={clsx(styles.badge, styles.warning)}>Security fix</span>
                  )}
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
      {result !== undefined && (
        <details>
          <summary>What {label} printed</summary>
          <pre className={updateStyles.output}>{result.output || 'Nothing.'}</pre>
        </details>
      )}
      <ConfirmDialog
        open={confirmAll}
        onOpenChange={setConfirmAll}
        title={`Install every ${label} update?`}
        description={`${countOf(source.updates.length, 'update', 'updates')} will be installed on ${machine.name}. Services may restart while they do.`}
        confirmLabel="Install all"
        onConfirm={() => {
          run([]);
        }}
      />
      <StepUpDialog
        open={stepUp.isOpen}
        onOpenChange={stepUp.setOpen}
        secondFactors={session.secondFactors}
        onConfirmed={stepUp.confirmed}
      />
    </section>
  );
}

function securityText(source: PackageSource): string {
  const security = source.updates.filter((update) => update.security).length;
  return security === 0 ? '' : `, ${countOf(security, 'security fix', 'security fixes')}`;
}

function versionText(current: string, available: string): string {
  if (current !== '' && available !== '') {
    return `${current} → ${available}`;
  }
  return available;
}
