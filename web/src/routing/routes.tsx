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
        children: [
          {
            index: true,
            lazy: async () => ({
              Component: (await import('../pages/Machine/MachineOverview')).MachineOverview,
            }),
          },
          {
            path: 'terminal',
            // Separate, so only people who open a terminal load xterm.js.
            lazy: async () => ({
              Component: (await import('../pages/Machine/TerminalTab')).TerminalTab,
            }),
          },
          {
            path: 'services',
            lazy: async () => ({
              Component: (await import('../pages/Machine/ServicesTab')).ServicesTab,
            }),
          },
          {
            path: 'processes',
            lazy: async () => ({
              Component: (await import('../pages/Machine/ProcessesTab')).ProcessesTab,
            }),
          },
          {
            path: 'logs',
            lazy: async () => ({ Component: (await import('../pages/Machine/LogsTab')).LogsTab }),
          },
          {
            path: 'files',
            lazy: async () => ({
              Component: (await import('../pages/Machine/files/FilesTab')).FilesTab,
            }),
          },
          {
            path: 'storage',
            lazy: async () => ({
              Component: (await import('../pages/Machine/storage/StorageTab')).StorageTab,
            }),
          },
          {
            path: 'docker',
            lazy: async () => ({
              Component: (await import('../pages/Machine/docker/DockerTab')).DockerTab,
            }),
            children: [
              {
                index: true,
                lazy: async () => ({
                  Component: (await import('../pages/Machine/docker/ContainersView'))
                    .ContainersView,
                }),
              },
              {
                path: 'containers/:container/logs',
                lazy: async () => ({
                  Component: (await import('../pages/Machine/docker/ContainerLogsView'))
                    .ContainerLogsView,
                }),
              },
              {
                path: 'containers/:container/shell',
                // With the terminal, so xterm.js loads only when it's opened.
                lazy: async () => ({
                  Component: (await import('../pages/Machine/docker/ContainerShellView'))
                    .ContainerShellView,
                }),
              },
              {
                path: 'images',
                lazy: async () => ({
                  Component: (await import('../pages/Machine/docker/ImagesView')).ImagesView,
                }),
              },
              {
                path: 'volumes',
                lazy: async () => ({
                  Component: (await import('../pages/Machine/docker/ResourcesView')).ResourcesView,
                }),
              },
              {
                path: 'stacks',
                lazy: async () => ({
                  Component: (await import('../pages/Machine/docker/StacksView')).StacksView,
                }),
              },
              {
                path: 'stacks/:stack',
                // With CodeMirror, so it loads only when a stack is opened.
                lazy: async () => ({
                  Component: (await import('../pages/Machine/docker/StackEditor')).StackEditor,
                }),
              },
            ],
          },
        ],
      },
      {
        path: '/account',
        lazy: async () => ({
          Component: (await import('../pages/Account/AccountPage')).AccountPage,
        }),
      },
      {
        path: '/scripts',
        lazy: async () => ({
          Component: (await import('../pages/Scripts/ScriptsPage')).ScriptsPage,
        }),
        children: [
          {
            index: true,
            lazy: async () => ({
              Component: (await import('../pages/Scripts/LibraryView')).LibraryView,
            }),
          },
          {
            path: 'runs',
            lazy: async () => ({ Component: (await import('../pages/Scripts/RunsView')).RunsView }),
          },
          {
            path: 'runs/:batchId',
            lazy: async () => ({
              Component: (await import('../pages/Scripts/BatchView')).BatchView,
            }),
          },
          {
            path: 'schedules',
            lazy: async () => ({
              Component: (await import('../pages/Scripts/SchedulesView')).SchedulesView,
            }),
          },
          {
            path: ':scriptId',
            lazy: async () => ({
              Component: (await import('../pages/Scripts/ScriptEditor')).ScriptEditor,
            }),
          },
        ],
      },
      {
        path: '/alerts',
        lazy: async () => ({
          Component: (await import('../pages/Alerts/AlertsPage')).AlertsPage,
        }),
        children: [
          {
            index: true,
            lazy: async () => ({
              Component: (await import('../pages/Alerts/AlertsView')).AlertsView,
            }),
          },
          {
            path: 'rules',
            lazy: async () => ({
              Component: (await import('../pages/Alerts/RulesView')).RulesView,
            }),
          },
          {
            path: 'channels',
            lazy: async () => ({
              Component: (await import('../pages/Alerts/ChannelsView')).ChannelsView,
            }),
          },
        ],
      },
      {
        path: '/users',
        lazy: async () => ({ Component: (await import('../pages/Users/UsersPage')).UsersPage }),
      },
      {
        path: '/audit',
        lazy: async () => ({ Component: (await import('../pages/Audit/AuditPage')).AuditPage }),
      },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
];
