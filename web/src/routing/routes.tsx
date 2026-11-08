// SPDX-License-Identifier: Apache-2.0
import type { RouteObject } from 'react-router';

import { AppShell } from '../components/AppShell/AppShell';
import { EnrollPage } from '../pages/Enroll/EnrollPage';
import { LoginPage } from '../pages/Login/LoginPage';
import { SecondFactorPage } from '../pages/Login/SecondFactorPage';
import { NotFoundPage } from '../pages/NotFound/NotFoundPage';
import { FleetPage } from '../pages/Fleet/FleetPage';
import { SetupPage } from '../pages/Setup/SetupPage';
import { AreaGate } from './AreaGate';

/** routes maps every URL of the web UI to its page. */
export const routes: RouteObject[] = [
  { path: '/setup', element: <AreaGate area="setup">{() => <SetupPage />}</AreaGate> },
  { path: '/login', element: <AreaGate area="login">{() => <LoginPage />}</AreaGate> },
  {
    path: '/login/verify',
    element: (
      <AreaGate area="second-factor">
        {(session) => session !== null && <SecondFactorPage session={session} />}
      </AreaGate>
    ),
  },
  { path: '/login/enroll', element: <AreaGate area="enroll">{() => <EnrollPage />}</AreaGate> },
  {
    element: (
      <AreaGate area="app">
        {(session) => session !== null && <AppShell session={session} />}
      </AreaGate>
    ),
    children: [
      { index: true, element: <FleetPage /> },
      // Loaded on first visit, so signing in doesn't wait for them (the
      // machine page also brings the chart libraries).
      {
        path: '/machines/add',
        lazy: async () => ({
          Component: (await import('../pages/AddMachine/AddMachinePage')).AddMachinePage,
        }),
      },
      {
        path: '/machines/:id',
        lazy: async () => ({
          Component: (await import('../pages/Machine/MachinePage')).MachinePage,
        }),
      },
      {
        path: '/account',
        lazy: async () => ({
          Component: (await import('../pages/Account/AccountPage')).AccountPage,
        }),
      },
      {
        path: '/audit',
        lazy: async () => ({ Component: (await import('../pages/Audit/AuditPage')).AuditPage }),
      },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
];
