// SPDX-License-Identifier: Apache-2.0
import { toast } from 'sonner';

import { useWake } from '../../api/system';
import type { AgentDetails } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { messageFor } from '../../components/FormError/messageFor';

/**
 * WakeAction wakes an offline machine with Wake-on-LAN, through an online
 * machine on its network (§9.6). The server only offers it when one can.
 */
export function WakeAction({ machine }: { machine: AgentDetails }) {
  const wake = useWake(machine.id);
  return (
    <Button
      variant="secondary"
      isBusy={wake.isPending}
      onClick={() => {
        wake.mutate(undefined, {
          onSuccess: (response) =>
            toast.success(
              `Sent a wake-up through ${response.via}. It may take a minute to come online.`,
            ),
          onError: (error) => toast.error(messageFor(error)),
        });
      }}
    >
      Wake
    </Button>
  );
}
