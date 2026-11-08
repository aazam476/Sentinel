// SPDX-License-Identifier: Apache-2.0
import { Menu } from '@base-ui/react/menu';
import clsx from 'clsx';
import { useDeferredValue, useState } from 'react';
import { useNavigate } from 'react-router';
import { Virtuoso } from 'react-virtuoso';
import { toast } from 'sonner';

import { useServiceAction, useServices, type ServiceAction } from '../../api/system';
import type { MachineService } from '../../api/types.gen';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import { countOf, formatNumber } from '../../formatting/formatting';
import { canOperate, useMachine } from './machineContext';
import styles from './SystemTab.module.css';

type StateFilter = 'all' | 'running' | 'failed';

const stateLabels: Record<MachineService['state'], string> = {
  running: 'Running',
  stopped: 'Stopped',
  failed: 'Failed',
  starting: 'Starting',
  stopping: 'Stopping',
  unknown: 'Unknown',
};

const startupLabels: Record<MachineService['startup'], string> = {
  automatic: 'Starts at boot',
  manual: 'Starts manually',
  disabled: 'Disabled',
  fixed: 'Started by the system',
};

const actionLabels: Record<ServiceAction, string> = {
  start: 'Start',
  stop: 'Stop',
  restart: 'Restart',
  enable: 'Start at boot',
  disable: 'Don’t start at boot',
};

/** Actions that interrupt something running ask first. */
const confirmed: Partial<Record<ServiceAction, string>> = {
  stop: 'Stop',
  disable: 'Disable',
};

/** ServicesTab lists the machine's services, and lets operators control them. */
export function ServicesTab() {
  const { session, machine } = useMachine();
  const services = useServices(machine.id);
  const action = useServiceAction(machine.id);
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<StateFilter>('all');
  const [toConfirm, setToConfirm] = useState<{ name: string; action: ServiceAction }>();
  const deferredSearch = useDeferredValue(search.trim().toLowerCase());

  if (!machine.online) {
    return (
      <p className={styles.muted}>The machine is offline. Its services show when it’s back.</p>
    );
  }
  if (services.isPending) {
    return <p className={styles.muted}>Loading services…</p>;
  }
  if (services.data === undefined) {
    return <FormError error={services.error} />;
  }

  const run = (name: string, serviceAction: ServiceAction) => {
    action.mutate(
      { name, action: serviceAction },
      {
        onSuccess: () => toast.success(`${actionLabels[serviceAction]}: ${name}`),
        onError: (error) => toast.error(messageFor(error)),
      },
    );
  };
  const choose = (name: string, serviceAction: ServiceAction) => {
    if (serviceAction in confirmed) {
      setToConfirm({ name, action: serviceAction });
    } else {
      run(name, serviceAction);
    }
  };

  const shown = services.data.services.filter(
    (service) =>
      (filter === 'all' || service.state === filter) &&
      (deferredSearch === '' ||
        service.name.toLowerCase().includes(deferredSearch) ||
        service.description.toLowerCase().includes(deferredSearch)),
  );
  return (
    <section className={styles.tab} aria-label="Services">
      <div className={styles.toolbar}>
        <input
          type="search"
          className={styles.search}
          placeholder="Search services"
          aria-label="Search services"
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
          aria-label="Show"
          value={filter}
          onChange={(event) => {
            setFilter(event.target.value as StateFilter);
          }}
        >
          <option value="all">All</option>
          <option value="running">Running</option>
          <option value="failed">Failed</option>
        </select>
      </div>
      <p className={styles.count}>{countText(shown.length, services.data.services.length)}</p>
      {shown.length > 0 && (
        <Virtuoso
          useWindowScroll
          data={shown}
          computeItemKey={(_, service) => service.name}
          className={styles.list}
          itemContent={(_, service) => (
            <ServiceRow
              service={service}
              canControl={canOperate(session)}
              onAction={choose}
              onLogs={(name) => {
                void navigate(`../logs?service=${encodeURIComponent(name)}`);
              }}
            />
          )}
        />
      )}
      <ConfirmDialog
        open={toConfirm !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setToConfirm(undefined);
          }
        }}
        title={
          toConfirm === undefined ? '' : `${actionLabels[toConfirm.action]} ${toConfirm.name}?`
        }
        description={
          toConfirm?.action === 'stop'
            ? 'Whatever it provides stops working until it is started again.'
            : 'It won’t start the next time the machine boots.'
        }
        confirmLabel={toConfirm === undefined ? '' : (confirmed[toConfirm.action] ?? '')}
        onConfirm={() => {
          if (toConfirm !== undefined) {
            run(toConfirm.name, toConfirm.action);
          }
        }}
      />
    </section>
  );
}

function countText(shown: number, total: number): string {
  const all = countOf(total, 'service', 'services');
  return shown === total ? all : `${formatNumber(shown)} of ${all}`;
}

interface ServiceRowProps {
  service: MachineService;
  canControl: boolean;
  onAction: (name: string, action: ServiceAction) => void;
  onLogs: (name: string) => void;
}

function ServiceRow({ service, canControl, onAction, onLogs }: ServiceRowProps) {
  return (
    <article className={styles.row} aria-label={service.name}>
      <div className={styles.rowMain}>
        <span className={styles.name}>{service.name}</span>
        {service.description !== '' && (
          <span className={styles.description} title={service.description}>
            {service.description}
          </span>
        )}
        <span className={styles.meta}>
          <span className={clsx(styles.badge, styles[service.state])}>
            {stateLabels[service.state]}
          </span>
          <span>{startupLabels[service.startup]}</span>
        </span>
      </div>
      <Menu.Root>
        <Menu.Trigger className={styles.menuTrigger} aria-label={`Actions for ${service.name}`}>
          ⋯
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner align="end" sideOffset={4}>
            <Menu.Popup className={styles.menu}>
              {canControl &&
                actionsFor(service).map((action) => (
                  <Menu.Item
                    key={action}
                    className={clsx(styles.menuItem, action in confirmed && styles.menuItemDanger)}
                    onClick={() => {
                      onAction(service.name, action);
                    }}
                  >
                    {actionLabels[action]}
                  </Menu.Item>
                ))}
              <Menu.Item
                className={styles.menuItem}
                onClick={() => {
                  onLogs(service.name);
                }}
              >
                View logs
              </Menu.Item>
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
    </article>
  );
}

/** actionsFor offers only what can work for the service as it is now. */
function actionsFor(service: MachineService): ServiceAction[] {
  const actions: ServiceAction[] = [];
  const isUp = service.state === 'running' || service.state === 'starting';
  if (!isUp && service.startup !== 'disabled') {
    actions.push('start');
  }
  if (isUp) {
    actions.push('restart', 'stop');
  }
  if (service.startup === 'manual' || service.startup === 'disabled') {
    actions.push('enable');
  }
  if (service.startup === 'automatic') {
    actions.push('disable');
  }
  return actions;
}
