// SPDX-License-Identifier: Apache-2.0
import { useOutletContext } from 'react-router';

import type { AgentDetails, SessionResponse } from '../../api/types.gen';

/** MachineContext is what the machine layout gives its tabs. */
export interface MachineContext {
  session: SessionResponse;
  machine: AgentDetails;
}

/** useMachine reads the machine and session in a machine tab. */
export function useMachine(): MachineContext {
  return useOutletContext<MachineContext>();
}

/** canOperate reports whether the signed-in user may act on machines. */
export function canOperate(session: SessionResponse): boolean {
  return session.user.role === 'operator' || session.user.role === 'admin';
}

/** allows reports whether the machine's local policy allows a capability. */
export function allows(machine: AgentDetails, capability: string): boolean {
  return machine.capabilities.find((c) => c.name === capability)?.enabled ?? true;
}
