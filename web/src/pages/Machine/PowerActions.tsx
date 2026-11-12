// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { toast } from 'sonner';

import { messageFor } from '../../components/FormError/messageFor';
import { usePower, type PowerAction } from '../../api/system';
import type { AgentDetails } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import styles from './MachinePage.module.css';

/**
 * PowerPhase follows a reboot or shutdown: requested (the machine accepted
 * and is still connected), then down (it went offline). A rebooted machine
 * coming back online ends it.
 */
type PowerPhase = { action: PowerAction; isDown: boolean } | undefined;

const copy = {
  reboot: {
    label: 'Reboot',
    title: 'Reboot this machine?',
    description:
      'Everything running on it stops, and anyone using it is disconnected until it comes back.',
    waiting: 'Rebooting. Waiting for the machine to come back…',
  },
  shutdown: {
    label: 'Shut down',
    title: 'Shut down this machine?',
    description:
      'It stays off until someone turns it on, or Sentinel wakes it through another machine on its network.',
    waiting: 'Shutting down…',
  },
} as const;

/** PowerActions reboots or shuts down the machine, then follows it. */
export function PowerActions({ machine }: { machine: AgentDetails }) {
  const power = usePower(machine.id);
  const [confirming, setConfirming] = useState<PowerAction | undefined>();
  const [phase, setPhase] = useState<PowerPhase>();
  const [wasOnline, setWasOnline] = useState(machine.online);

  // Follow the machine's status as the event stream updates it (state
  // adjusted while rendering, as React recommends, rather than an effect).
  if (machine.online !== wasOnline) {
    setWasOnline(machine.online);
    if (phase !== undefined && !machine.online) {
      setPhase({ ...phase, isDown: true });
    } else if (phase?.action === 'reboot' && phase.isDown && machine.online) {
      setPhase(undefined);
      toast.success(`${machine.name} is back online`);
    }
  }

  const run = (action: PowerAction) => {
    power.mutate(action, {
      onSuccess: () => {
        setPhase({ action, isDown: false });
      },
      onError: (error) => toast.error(messageFor(error)),
    });
  };

  if (phase !== undefined && !(phase.action === 'shutdown' && phase.isDown)) {
    return (
      <p className={styles.powerStatus} role="status">
        {copy[phase.action].waiting}
      </p>
    );
  }
  return (
    <div className={styles.powerActions}>
      {(['reboot', 'shutdown'] as const).map((action) => (
        <Button
          key={action}
          variant="secondary"
          disabled={!machine.online}
          isBusy={power.isPending && power.variables === action}
          onClick={() => {
            setConfirming(action);
          }}
        >
          {copy[action].label}
        </Button>
      ))}
      <ConfirmDialog
        open={confirming !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setConfirming(undefined);
          }
        }}
        title={confirming === undefined ? '' : copy[confirming].title}
        description={confirming === undefined ? '' : copy[confirming].description}
        confirmLabel={confirming === undefined ? '' : copy[confirming].label}
        onConfirm={() => {
          if (confirming !== undefined) {
            run(confirming);
          }
        }}
      />
    </div>
  );
}
