// SPDX-License-Identifier: Apache-2.0
import type { StorageOverviewPayload } from '../../../api/protocol.gen';
import { useStorage } from '../../../api/storage';
import { FormError } from '../../../components/FormError/FormError';
import { allows, useMachine } from '../machineContext';
import { DatasetTree } from './DatasetTree';
import { DiskList } from './DiskList';
import { OtherFilesystems } from './OtherFilesystems';
import { PoolCards } from './PoolCards';
import styles from './Storage.module.css';
import { StorageAlerts } from './StorageAlerts';
import { VdevLayout } from './VdevLayout';

/**
 * StorageTab shows how the machine's space is really laid out (§9.5): ZFS
 * pools, datasets and vdevs, physical disks and other filesystems. It is
 * read-only. It reads when opened and every minute while open.
 */
export function StorageTab() {
  const { machine } = useMachine();
  const isAllowed = allows(machine, 'storage');
  const storage = useStorage(machine.id, isAllowed && machine.online);
  return (
    <div className={styles.tab}>
      <StorageAlerts agentId={machine.id} />
      {!isAllowed ? (
        <p className={styles.muted}>Storage is disabled by this machine’s local policy.</p>
      ) : !machine.online ? (
        <p className={styles.muted}>The machine is offline. Its storage shows when it’s back.</p>
      ) : storage.isPending ? (
        <p className={styles.muted}>Reading the storage…</p>
      ) : storage.data === undefined ? (
        <FormError error={storage.error} />
      ) : (
        <StorageSections overview={storage.data} />
      )}
    </div>
  );
}

function StorageSections({ overview }: { overview: StorageOverviewPayload }) {
  const { zfs, pools } = overview;
  return (
    <>
      {zfs.problem !== undefined && zfs.problem !== '' && (
        <p className={styles.problem}>ZFS is installed but couldn’t be read: {zfs.problem}</p>
      )}
      {zfs.installed && pools.length === 0 && (zfs.problem ?? '') === '' && (
        <p className={styles.muted}>ZFS is installed, with no pools.</p>
      )}
      {pools.length > 0 && (
        <>
          <PoolCards pools={pools} arc={overview.arc} />
          <DatasetTree datasets={overview.datasets} />
          <VdevLayout pools={pools} />
        </>
      )}
      <DiskList disks={overview.disks} smartAvailable={overview.smartAvailable} />
      <OtherFilesystems filesystems={overview.filesystems} />
    </>
  );
}
