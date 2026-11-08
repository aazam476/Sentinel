// SPDX-License-Identifier: Apache-2.0
import { Menu } from '@base-ui/react/menu';
import clsx from 'clsx';
import { useDeferredValue, useState } from 'react';
import { useNavigate } from 'react-router';
import { Virtuoso } from 'react-virtuoso';
import { toast } from 'sonner';

import {
  useContainerAction,
  useContainers,
  useRemoveContainer,
  type ContainerAction,
} from '../../../api/docker';
import type { Container } from '../../../api/protocol.gen';
import { ConfirmDialog } from '../../../components/ConfirmDialog/ConfirmDialog';
import { messageFor } from '../../../components/FormError/messageFor';
import { countOf, formatBytes, formatNumber } from '../../../formatting/formatting';
import { canOperate, useMachine } from '../machineContext';
import styles from '../SystemTab.module.css';
import { DockerError } from './dockerError';

const actionLabels: Record<ContainerAction, string> = {
  start: 'Start',
  stop: 'Stop',
  restart: 'Restart',
};

/** ContainersView lists the machine's containers and controls them. */
export function ContainersView() {
  const { session, machine } = useMachine();
  const containers = useContainers(machine.id);
  const action = useContainerAction(machine.id);
  const remove = useRemoveContainer(machine.id);
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [toRemove, setToRemove] = useState<Container>();
  const deferredSearch = useDeferredValue(search.trim().toLowerCase());

  if (containers.isPending) {
    return <p className={styles.muted}>Loading containers…</p>;
  }
  if (containers.data === undefined) {
    return <DockerError error={containers.error} />;
  }
  const all = containers.data.containers;
  const shown = all.filter(
    (c) =>
      deferredSearch === '' ||
      [c.name, c.image, c.stack ?? ''].some((text) => text.toLowerCase().includes(deferredSearch)),
  );
  const run = (container: Container, verb: ContainerAction) => {
    action.mutate(
      { name: container.name, action: verb },
      {
        onSuccess: () => toast.success(`${actionLabels[verb]}: ${container.name}`),
        onError: (error) => toast.error(messageFor(error)),
      },
    );
  };
  return (
    <section className={styles.tab} aria-label="Containers">
      <div className={styles.toolbar}>
        <input
          type="search"
          className={styles.search}
          placeholder="Search by name, image or stack"
          aria-label="Search containers"
          value={search}
          autoCapitalize="none"
          autoCorrect="off"
          enterKeyHint="search"
          onChange={(event) => {
            setSearch(event.target.value);
          }}
        />
      </div>
      <p className={styles.count}>
        {shown.length === all.length
          ? countOf(all.length, 'container', 'containers')
          : `${formatNumber(shown.length)} of ${countOf(all.length, 'container', 'containers')}`}
        {' · '}
        {containers.data.engine.name} {containers.data.engine.version}
      </p>
      {all.length === 0 && (
        <p className={styles.muted}>No containers yet. Create a stack to start some.</p>
      )}
      {shown.length > 0 && (
        <Virtuoso
          useWindowScroll
          data={shown}
          computeItemKey={(_, c) => c.id}
          className={styles.list}
          itemContent={(_, c) => (
            <ContainerRow
              container={c}
              canControl={canOperate(session)}
              onAction={(verb) => {
                run(c, verb);
              }}
              onRemove={() => {
                setToRemove(c);
              }}
              onOpen={(page) => {
                void navigate(`containers/${encodeURIComponent(c.name)}/${page}`);
              }}
            />
          )}
        />
      )}
      <ConfirmDialog
        open={toRemove !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setToRemove(undefined);
          }
        }}
        title={`Remove ${toRemove?.name ?? ''}?`}
        description={
          toRemove?.state === 'running'
            ? 'It is running: it will be stopped and removed. Its volumes are kept.'
            : 'Its volumes are kept.'
        }
        confirmLabel="Remove"
        onConfirm={() => {
          if (toRemove === undefined) {
            return;
          }
          const target = toRemove;
          remove.mutate(
            { name: target.name, force: target.state === 'running' },
            {
              onSuccess: () => toast.success(`Removed ${target.name}`),
              onError: (error) => toast.error(messageFor(error)),
            },
          );
        }}
      />
    </section>
  );
}

interface ContainerRowProps {
  container: Container;
  canControl: boolean;
  onAction: (action: ContainerAction) => void;
  onRemove: () => void;
  onOpen: (page: 'logs' | 'shell') => void;
}

function ContainerRow({ container, canControl, onAction, onRemove, onOpen }: ContainerRowProps) {
  const isRunning = container.state === 'running';
  const published = container.ports
    .filter((port) => port.publicPort !== undefined && port.publicPort !== 0)
    .map((port) => `${String(port.publicPort)}→${String(port.privatePort)}/${port.protocol}`);
  return (
    <article className={styles.row} aria-label={container.name}>
      <div className={styles.rowMain}>
        <span className={styles.name}>{container.name}</span>
        <span className={styles.description} title={container.image}>
          {container.image}
          {container.stack !== undefined && container.stack !== '' && ` · ${container.stack}`}
        </span>
        <span className={styles.meta}>
          <span
            className={clsx(
              styles.badge,
              isRunning && styles.running,
              (container.state === 'dead' || container.state === 'exited') && styles.failed,
            )}
            title={container.status}
          >
            {container.state}
          </span>
          {isRunning && <span>CPU {container.cpuPercent.toFixed(1)}%</span>}
          {isRunning && <span>{formatBytes(container.memoryBytes)}</span>}
          {published.length > 0 && (
            <span title={published.join(', ')}>{published.slice(0, 2).join(', ')}</span>
          )}
        </span>
      </div>
      <Menu.Root>
        <Menu.Trigger className={styles.menuTrigger} aria-label={`Actions for ${container.name}`}>
          ⋯
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner align="end" sideOffset={4}>
            <Menu.Popup className={styles.menu}>
              <Menu.Item
                className={styles.menuItem}
                onClick={() => {
                  onOpen('logs');
                }}
              >
                Logs
              </Menu.Item>
              {canControl && isRunning && (
                <Menu.Item
                  className={styles.menuItem}
                  onClick={() => {
                    onOpen('shell');
                  }}
                >
                  Shell
                </Menu.Item>
              )}
              {canControl &&
                (isRunning ? (['restart', 'stop'] as const) : (['start'] as const)).map((verb) => (
                  <Menu.Item
                    key={verb}
                    className={styles.menuItem}
                    onClick={() => {
                      onAction(verb);
                    }}
                  >
                    {actionLabels[verb]}
                  </Menu.Item>
                ))}
              {canControl && (
                <Menu.Item
                  className={clsx(styles.menuItem, styles.menuItemDanger)}
                  onClick={onRemove}
                >
                  Remove
                </Menu.Item>
              )}
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
    </article>
  );
}
