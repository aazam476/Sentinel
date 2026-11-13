// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type {
  AgentDetails,
  ApplyPackagesRequest,
  PackageUpdatesResponse,
} from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { adminSession, nasDetails, viewerSession } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const base = `/machines/${nasDetails.id}`;

const waiting: PackageUpdatesResponse = {
  restartRequired: true,
  sources: [
    {
      manager: 'apt',
      selectable: true,
      error: '',
      updates: [
        { id: 'openssl', name: 'openssl', current: '3.0.13', available: '3.0.14', security: true },
        { id: 'tzdata', name: 'tzdata', current: '', available: '2024b', security: false },
      ],
    },
    { manager: 'homebrew', selectable: true, error: 'brew: can’t reach GitHub', updates: [] },
  ],
};

function withUpdates(
  list: PackageUpdatesResponse,
  install: (request: ApplyPackagesRequest) => Response = () =>
    json({ output: 'Setting up openssl (3.0.14)', restartRequired: true }),
  details: AgentDetails = nasDetails,
) {
  const installs: ApplyPackagesRequest[] = [];
  server.use(
    http.get(apiPath(`agents/${details.id}`), () => json(details)),
    http.get(apiPath(`agents/${details.id}/metrics`), () =>
      json({ metric: 'cpu', resolution: 'raw', points: [] }),
    ),
    http.get(apiPath(`agents/${details.id}/packages`), () => json(list)),
    http.post(apiPath(`agents/${details.id}/packages`), async ({ request }) => {
      const body = (await request.json()) as ApplyPackagesRequest;
      installs.push(body);
      return install(body);
    }),
  );
  return installs;
}

const machineTab = (name: string) =>
  within(screen.getByRole('navigation', { name: 'Machine sections' })).queryByRole('link', {
    name,
  });

describe('§9.6 package updates', () => {
  it('lists each manager’s updates, with security fixes and a waiting restart', async () => {
    signedInAs(adminSession);
    withUpdates(waiting);
    const { container } = renderApp(`${base}/updates`);
    const apt = await screen.findByRole('region', { name: 'APT' });
    expect(within(apt).getByText('2 updates available, 1 security fix.')).toBeVisible();
    expect(within(apt).getByText('3.0.13 → 3.0.14')).toBeVisible();
    expect(within(apt).getByText('Security fix')).toBeVisible();
    expect(within(apt).getByText('2024b')).toBeVisible();
    expect(
      screen.getByText('Installed updates are waiting for the machine to restart.'),
    ).toBeVisible();
    expect(
      within(screen.getByRole('region', { name: 'Homebrew' })).getByRole('alert'),
    ).toHaveTextContent('Homebrew couldn’t be checked: brew: can’t reach GitHub');
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('installs the chosen updates and shows what the manager printed', async () => {
    signedInAs(adminSession);
    const installs = withUpdates(waiting);
    renderApp(`${base}/updates`);
    const apt = await screen.findByRole('region', { name: 'APT' });
    await userEvent.click(within(apt).getByRole('checkbox', { name: 'Choose openssl' }));
    await userEvent.click(within(apt).getByRole('button', { name: 'Install 1 update' }));
    expect(await screen.findByText('APT: updates installed')).toBeVisible();
    expect(installs).toEqual([{ manager: 'apt', ids: ['openssl'] }]);
    await userEvent.click(within(apt).getByText('What APT printed'));
    expect(within(apt).getByText('Setting up openssl (3.0.14)')).toBeVisible();
  });

  it('asks before installing everything, and installs all at once where that’s the only way', async () => {
    signedInAs(adminSession);
    const installs = withUpdates({
      restartRequired: false,
      sources: [
        {
          manager: 'pacman',
          selectable: false,
          error: '',
          updates: [
            { id: 'linux', name: 'linux', current: '6.8', available: '6.9', security: false },
          ],
        },
      ],
    });
    renderApp(`${base}/updates`);
    const pacman = await screen.findByRole('region', { name: 'pacman' });
    expect(within(pacman).queryByRole('checkbox')).not.toBeInTheDocument();
    expect(within(pacman).getByText(/installs them all at once/)).toBeVisible();
    await userEvent.click(within(pacman).getByRole('button', { name: 'Install all' }));
    const dialog = await screen.findByRole('alertdialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Install all' }));
    expect(await screen.findByText('pacman: updates installed')).toBeVisible();
    expect(installs).toEqual([{ manager: 'pacman', ids: [] }]);
  });

  it('SEC-07: asks to confirm it’s you before installing', async () => {
    signedInAs(adminSession);
    withUpdates(waiting, () => apiError(403, 'step_up_required', 'Confirm it’s you first.'));
    renderApp(`${base}/updates`);
    const apt = await screen.findByRole('region', { name: 'APT' });
    await userEvent.click(within(apt).getByRole('button', { name: 'Install all' }));
    await userEvent.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Install all' }),
    );
    expect(await screen.findByRole('dialog')).toBeVisible();
  });

  it('shows a failed install’s reason', async () => {
    signedInAs(adminSession);
    withUpdates(waiting, () =>
      apiError(502, 'agent_failed', 'The agent couldn’t install the updates: E: Unable to lock'),
    );
    renderApp(`${base}/updates`);
    const apt = await screen.findByRole('region', { name: 'APT' });
    await userEvent.click(within(apt).getByRole('checkbox', { name: 'Choose tzdata' }));
    await userEvent.click(within(apt).getByRole('button', { name: 'Install 1 update' }));
    expect(await screen.findByText(/E: Unable to lock/)).toBeVisible();
  });

  it('says a machine is up to date, and checks again on request', async () => {
    signedInAs(adminSession);
    withUpdates({
      restartRequired: false,
      sources: [{ manager: 'windows-update', selectable: true, error: '', updates: [] }],
    });
    renderApp(`${base}/updates`);
    const windows = await screen.findByRole('region', { name: 'Windows Update' });
    expect(within(windows).getByText('Up to date.')).toBeVisible();
    expect(within(windows).queryByRole('button')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }));
    expect(await screen.findByRole('region', { name: 'Windows Update' })).toBeVisible();
  });

  it('says why a machine can’t be checked', async () => {
    signedInAs(adminSession);
    server.use(
      http.get(apiPath(`agents/${nasDetails.id}`), () => json(nasDetails)),
      http.get(apiPath(`agents/${nasDetails.id}/packages`), () =>
        apiError(504, 'machine_timeout', 'The machine didn’t answer in time.'),
      ),
    );
    renderApp(`${base}/updates`);
    expect(await screen.findByText('The machine didn’t answer in time.')).toBeVisible();
  });

  it('waits for an offline machine', async () => {
    signedInAs(adminSession);
    withUpdates(waiting, undefined, { ...nasDetails, online: false });
    renderApp(`${base}/updates`);
    expect(await screen.findByText(/The machine is offline/)).toBeVisible();
  });

  it('hides the tab from viewers, and where the machine can’t update packages', async () => {
    signedInAs(viewerSession);
    withUpdates(waiting);
    const { unmount } = renderApp(base);
    await screen.findByRole('heading', { name: nasDetails.name });
    expect(machineTab('Updates')).not.toBeInTheDocument();
    unmount();
    signedInAs(adminSession);
    withUpdates(waiting, undefined, {
      ...nasDetails,
      capabilities: [
        {
          name: 'packages',
          enabled: false,
          reason: 'This machine has no package manager Sentinel supports',
        },
      ],
    });
    renderApp(base);
    await screen.findByRole('heading', { name: nasDetails.name });
    expect(machineTab('Updates')).not.toBeInTheDocument();
  });
});
