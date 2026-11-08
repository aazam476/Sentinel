// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { toast } from 'sonner';

import { useNetworks, useRemoveNetwork, useRemoveVolume, useVolumes } from '../../../api/docker';
import { Button } from '../../../components/Button/Button';
import { ConfirmDialog } from '../../../components/ConfirmDialog/ConfirmDialog';
import { messageFor } from '../../../components/FormError/messageFor';
import { countOf } from '../../../formatting/formatting';
import { canOperate, useMachine } from '../machineContext';
import styles from '../SystemTab.module.css';
import { DockerError } from './dockerError';

type Removal = { kind: 'volume' | 'network'; name: string };

/**
 * ResourcesView lists volumes and networks. Operators can remove the ones
 * nothing uses; the built-in networks are never offered.
 */
export function ResourcesView() {
  const { session, machine } = useMachine();
  const volumes = useVolumes(machine.id);
  const networks = useNetworks(machine.id);
  const removeVolume = useRemoveVolume(machine.id);
  const removeNetwork = useRemoveNetwork(machine.id);
  const [toRemove, setToRemove] = useState<Removal>();
  const canControl = canOperate(session);

  if (volumes.isPending || networks.isPending) {
    return <p className={styles.muted}>Loading volumes and networks…</p>;
  }
  if (volumes.data === undefined || networks.data === undefined) {
    return <DockerError error={volumes.error ?? networks.error} />;
  }
  const removeButton = (removal: Removal, inUse: boolean) =>
    canControl && !inUse ? (
      <Button
        variant="ghost"
        aria-label={`Remove ${removal.kind} ${removal.name}`}
        onClick={() => {
          setToRemove(removal);
        }}
      >
        Remove
      </Button>
    ) : null;
  return (
    <section className={styles.tab} aria-label="Volumes and networks">
      <h2 className={styles.name}>Volumes</h2>
      <p className={styles.count}>{countOf(volumes.data.volumes.length, 'volume', 'volumes')}</p>
      <ul className={styles.list}>
        {volumes.data.volumes.map((volume) => (
          <li key={volume.name} className={styles.row}>
            <span className={styles.rowMain}>
              <span className={styles.name}>{volume.name}</span>
              <span className={styles.description} title={volume.mountpoint}>
                {volume.mountpoint}
              </span>
              <span className={styles.meta}>
                <span>{volume.inUse ? 'In use' : 'Unused'}</span>
                {volume.stack !== undefined && volume.stack !== '' && <span>{volume.stack}</span>}
              </span>
            </span>
            {removeButton({ kind: 'volume', name: volume.name }, volume.inUse)}
          </li>
        ))}
      </ul>
      <h2 className={styles.name}>Networks</h2>
      <p className={styles.count}>
        {countOf(networks.data.networks.length, 'network', 'networks')}
      </p>
      <ul className={styles.list}>
        {networks.data.networks.map((network) => (
          <li key={network.id} className={styles.row}>
            <span className={styles.rowMain}>
              <span className={styles.name}>{network.name}</span>
              <span className={styles.meta}>
                <span>{network.driver}</span>
                <span>{countOf(network.containers, 'container', 'containers')}</span>
                {network.builtIn && <span>Built in</span>}
              </span>
            </span>
            {removeButton(
              { kind: 'network', name: network.name },
              network.builtIn || network.containers > 0,
            )}
          </li>
        ))}
      </ul>
      <ConfirmDialog
        open={toRemove !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setToRemove(undefined);
          }
        }}
        title={`Remove the ${toRemove?.kind ?? ''} ${toRemove?.name ?? ''}?`}
        description={
          toRemove?.kind === 'volume'
            ? 'Its data is deleted for good.'
            : 'Stacks that expect it will create it again.'
        }
        confirmLabel="Remove"
        onConfirm={() => {
          if (toRemove === undefined) {
            return;
          }
          const { kind, name } = toRemove;
          const mutation = kind === 'volume' ? removeVolume : removeNetwork;
          mutation.mutate(name, {
            onSuccess: () => toast.success(`Removed ${name}`),
            onError: (error) => toast.error(messageFor(error)),
          });
        }}
      />
    </section>
  );
}
