// SPDX-License-Identifier: Apache-2.0
import { Menu } from '@base-ui/react/menu';
import clsx from 'clsx';
import { useDeferredValue, useState } from 'react';
import { Virtuoso } from 'react-virtuoso';
import { toast } from 'sonner';

import { useKillProcess, useProcesses } from '../../api/system';
import type { MachineProcess } from '../../api/types.gen';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import { countOf, formatBytes, formatNumber } from '../../formatting/formatting';
import { useMachine } from './machineContext';
import styles from './SystemTab.module.css';

type SortKey = 'cpu' | 'memory' | 'name';

const sorters: Record<SortKey, (a: MachineProcess, b: MachineProcess) => number> = {
  cpu: (a, b) => b.cpuPercent - a.cpuPercent || a.pid - b.pid,
  memory: (a, b) => b.memoryBytes - a.memoryBytes || a.pid - b.pid,
  name: (a, b) => a.name.localeCompare(b.name) || a.pid - b.pid,
};

/** ProcessesTab lists what runs on the machine, refreshed every 5 seconds. */
export function ProcessesTab() {
  const { machine } = useMachine();
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<SortKey>('cpu');
  const [toKill, setToKill] = useState<{ process: MachineProcess; force: boolean }>();
  // Refreshing pauses while a menu or dialog is open, so rows don't move
  // under the user's finger.
  const [isMenuOpen, setMenuOpen] = useState(false);
  const processes = useProcesses(machine.id, !machine.online || isMenuOpen || toKill !== undefined);
  const kill = useKillProcess(machine.id);
  const deferredSearch = useDeferredValue(search.trim().toLowerCase());

  if (!machine.online) {
    return (
      <p className={styles.muted}>The machine is offline. Its processes show when it’s back.</p>
    );
  }
  if (processes.isPending) {
    return <p className={styles.muted}>Measuring processes…</p>;
  }
  if (processes.data === undefined) {
    return <FormError error={processes.error} />;
  }

  const all = processes.data.processes;
  const shown = all
    .filter(
      (process) =>
        deferredSearch === '' ||
        process.name.toLowerCase().includes(deferredSearch) ||
        process.command.toLowerCase().includes(deferredSearch) ||
        process.user.toLowerCase().includes(deferredSearch) ||
        String(process.pid) === deferredSearch,
    )
    .sort(sorters[sort]);
  return (
    <section className={styles.tab} aria-label="Processes">
      <div className={styles.toolbar}>
        <input
          type="search"
          className={styles.search}
          placeholder="Search by name, command, user or PID"
          aria-label="Search processes"
          value={search}
          autoCapitalize="none"
          autoCorrect="off"
          enterKeyHint="search"
          onChange={(event) => {
            setSearch(event.target.value);
          }}
        />
        <select
          className={styles.select}
          aria-label="Sort by"
          value={sort}
          onChange={(event) => {
            setSort(event.target.value as SortKey);
          }}
        >
          <option value="cpu">CPU</option>
          <option value="memory">Memory</option>
          <option value="name">Name</option>
        </select>
      </div>
      <p className={styles.count}>
        {shown.length === all.length
          ? countOf(all.length, 'process', 'processes')
          : `${formatNumber(shown.length)} of ${countOf(all.length, 'process', 'processes')}`}
      </p>
      {shown.length > 0 && (
        <Virtuoso
          useWindowScroll
          data={shown}
          computeItemKey={(_, process) => process.pid}
          className={styles.list}
          itemContent={(_, process) => (
            <ProcessRow
              process={process}
              onMenuOpenChange={setMenuOpen}
              onKill={(force) => {
                setToKill({ process, force });
              }}
            />
          )}
        />
      )}
      <ConfirmDialog
        open={toKill !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setToKill(undefined);
          }
        }}
        title={
          toKill === undefined ? '' : `End ${toKill.process.name} (${String(toKill.process.pid)})?`
        }
        description={
          toKill?.force === true
            ? 'It is killed at once, without a chance to save its work.'
            : 'It is asked to stop, and may take a moment to save its work.'
        }
        confirmLabel={toKill?.force === true ? 'Kill' : 'End'}
        onConfirm={() => {
          if (toKill === undefined) {
            return;
          }
          const { process, force } = toKill;
          kill.mutate(
            { pid: process.pid, force },
            {
              onSuccess: () => toast.success(`Ended ${process.name} (${String(process.pid)})`),
              onError: (error) => toast.error(messageFor(error)),
            },
          );
        }}
      />
    </section>
  );
}

interface ProcessRowProps {
  process: MachineProcess;
  onMenuOpenChange: (open: boolean) => void;
  onKill: (force: boolean) => void;
}

function ProcessRow({ process, onMenuOpenChange, onKill }: ProcessRowProps) {
  const isBusy = process.cpuPercent >= 50;
  return (
    <article className={styles.row} aria-label={`${process.name} (${String(process.pid)})`}>
      <div className={styles.rowMain}>
        <span className={styles.name}>
          {process.name === '' ? 'Unnamed process' : process.name}
        </span>
        {process.command !== '' && (
          <span className={styles.description} title={process.command}>
            {process.command}
          </span>
        )}
        <span className={styles.meta}>
          <span className={clsx(styles.badge, isBusy && styles.warning)}>
            CPU {process.cpuPercent.toFixed(1)}%
          </span>
          <span>{formatBytes(process.memoryBytes)}</span>
          <span>PID {process.pid}</span>
          {process.user !== '' && <span>{process.user}</span>}
        </span>
      </div>
      <Menu.Root onOpenChange={onMenuOpenChange}>
        <Menu.Trigger
          className={styles.menuTrigger}
          aria-label={`Actions for ${process.name} (${String(process.pid)})`}
        >
          ⋯
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner align="end" sideOffset={4}>
            <Menu.Popup className={styles.menu}>
              <Menu.Item
                className={styles.menuItem}
                onClick={() => {
                  onKill(false);
                }}
              >
                End process
              </Menu.Item>
              <Menu.Item
                className={clsx(styles.menuItem, styles.menuItemDanger)}
                onClick={() => {
                  onKill(true);
                }}
              >
                Kill process
              </Menu.Item>
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
    </article>
  );
}
