// SPDX-License-Identifier: Apache-2.0
import type { ReactNode } from 'react';
import { Navigate } from 'react-router';

import { useSession } from '../api/session';
import { useSetupStatus } from '../api/setup';
import type { SessionResponse } from '../api/types.gen';
import { LoadingScreen } from '../components/LoadingScreen/LoadingScreen';
import { ErrorScreen } from './ErrorScreen';
import { areaFor, homeOf, type Area } from './destination';

interface AreaGateProps {
  area: Area;
  /** children renders the area's pages with the current session. */
  children: (session: SessionResponse | null) => ReactNode;
}

/**
 * AreaGate shows its pages only when the browser is in that area, and
 * sends it to the right area otherwise. The server enforces the same rules;
 * this only avoids showing pages that would fail.
 */
export function AreaGate({ area, children }: AreaGateProps) {
  const setupStatus = useSetupStatus();
  const session = useSession();

  if (setupStatus.isPending || session.isPending) {
    return <LoadingScreen />;
  }
  if (setupStatus.isError || session.isError) {
    return <ErrorScreen error={setupStatus.error ?? session.error} />;
  }

  const actual = areaFor(setupStatus.data.setupRequired, session.data);
  if (actual !== area) {
    return <Navigate to={homeOf[actual]} replace />;
  }
  return children(session.data);
}
