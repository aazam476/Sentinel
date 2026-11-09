// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type { StorageOverviewPayload } from '../../../api/protocol.gen';
import type { MachineAlerts } from '../../../api/types.gen';
import { findAccessibilityViolations } from '../../../test/axe';
import {
  fixtureNow,
  nasDetails,
  nasStorage,
  viewerSession,
} from '../../../test/fixtures/normal/fixtures';
import { renderApp } from '../../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../../test/server';

const base = `/machines/${nasDetails.id}/storage`;

function withStorage(
  storage: StorageOverviewPayload | null,
  alerts: MachineAlerts = { alerts: [] },
  machine = nasDetails,
) {
  signedInAs(viewerSession);
  server.use(
    http.get(apiPath(`agents/${nasDetails.id}`), () => json(machine)),
    http.get(apiPath(`agents/${nasDetails.id}/alerts`), () => json(alerts)),
    http.get(apiPath(`agents/${nasDetails.id}/storage`), () =>
      storage === null
        ? apiError(403, 'policy_denied', 'The storage view is disabled.')
        : json(storage),
    ),
  );
}

describe('Storage tab', () => {
  it('shows each pool with the numbers zpool list reports, and its last scrub', async () => {
    withStorage(nasStorage);
    const { container } = renderApp(base);

    const tank = await screen.findByRole('article', { name: 'Pool tank' });
    expect(within(tank).getByText('ONLINE')).toBeVisible();
    expect(within(tank).getByText('3.6 TB')).toHaveAttribute('title', '3,985,729,650,688 bytes');
    expect(within(tank).getByText('2.6 TB')).toHaveAttribute('title', '2,817,341,710,336 bytes');
    expect(within(tank).getByText('1.1 TB')).toHaveAttribute('title', '1,168,387,940,352 bytes');
    expect(within(tank).getByText('71%')).toBeVisible();
    expect(within(tank).getByText('12%')).toBeVisible();
    expect(within(tank).getAllByText('1.07×')).toHaveLength(2);
    expect(within(tank).getByText(/^Scrub finished .* · no errors$/)).toBeVisible();

    const backup = screen.getByRole('article', { name: 'Pool backup' });
    expect(within(backup).getByText('DEGRADED')).toBeVisible();
    expect(within(backup).getByText(/taken offline by the administrator/)).toBeVisible();
    expect(within(backup).getByText(/· 3 errors$/)).toBeVisible();
    expect(within(screen.getByRole('region', { name: 'ARC' })).getByText('99%')).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('shows the dataset tree, with quotas, zvols and the shared free space explained', async () => {
    withStorage(nasStorage);
    const user = userEvent.setup();
    renderApp(base);

    const datasets = await screen.findByRole('region', { name: 'Datasets' });
    const names = () =>
      within(datasets)
        .getAllByRole('listitem')
        .map((row) => row.getAttribute('aria-label'));
    expect(names()).toEqual([
      'Dataset tank',
      'Dataset tank/home',
      'Dataset tank/media',
      'Dataset tank/media/photos',
      'Volume tank/vm-disk',
    ]);
    const home = within(datasets).getByRole('listitem', { name: 'Dataset tank/home' });
    expect(within(home).getByText('Quota 100 GB')).toBeVisible();
    expect(within(home).getByText('85%')).toBeVisible();
    expect(within(datasets).getByText('volume · 50 GB')).toBeVisible();
    expect(within(datasets).getByText(/share their pool’s free space/)).toBeVisible();

    await user.click(within(datasets).getByRole('button', { name: 'Largest first' }));
    expect(names()).toEqual([
      'Dataset tank',
      'Dataset tank/media',
      'Dataset tank/media/photos',
      'Dataset tank/home',
      'Volume tank/vm-disk',
    ]);
  });

  it('shows the vdev tree with each member’s state and errors', async () => {
    withStorage(nasStorage);
    renderApp(base);
    const backup = await screen.findByRole('group', { name: 'Vdevs of backup' });
    expect(within(backup).getByText('0 read · 0 write · 2 checksum')).toBeVisible();
    expect(within(backup).getByText('OFFLINE')).toBeVisible();
    const tank = screen.getByRole('group', { name: 'Vdevs of tank' });
    expect(within(tank).getByText('Cache')).toBeVisible();
    expect(within(tank).getByText('AVAIL')).toBeVisible();
  });

  it('shows member disks as members of their pool instead of their own use, with SMART', async () => {
    withStorage(nasStorage);
    renderApp(base);
    const sdb = await screen.findByRole('listitem', { name: 'Disk sdb' });
    expect(within(sdb).getByText('Member of tank (mirror-0)')).toBeVisible();
    expect(within(sdb).getByText('SMART OK')).toBeVisible();
    expect(within(sdb).getByText('35 °C')).toBeVisible();
    expect(
      within(screen.getByRole('listitem', { name: 'Disk sda' })).getByText('Not in a pool'),
    ).toBeVisible();
    const sdd = screen.getByRole('listitem', { name: 'Disk sdd' });
    expect(within(sdd).getByText('SMART failing')).toBeVisible();
    expect(within(sdd).getByText('Reallocated_Sector_Ct failing · Spin_Retry_Count')).toBeVisible();
    expect(
      within(screen.getByRole('listitem', { name: 'Disk nvme0n1' })).getByText('SMART unknown'),
    ).toBeVisible();
  });

  it('hides system mounts until asked', async () => {
    withStorage(nasStorage);
    const user = userEvent.setup();
    renderApp(base);
    const filesystems = await screen.findByRole('region', { name: 'Other filesystems' });
    expect(within(filesystems).getAllByRole('listitem')).toHaveLength(2);
    await user.click(within(filesystems).getByRole('checkbox', { name: 'Show system mounts (1)' }));
    expect(
      within(filesystems).getByRole('listitem', { name: 'Filesystem /snap/core22/1380' }),
    ).toBeVisible();
  });

  it('lists firing storage alerts above everything else', async () => {
    withStorage(nasStorage, {
      alerts: [
        {
          id: 2,
          kind: 'zfs.pool.health',
          subject: 'backup',
          severity: 'warning',
          message: 'Pool backup is DEGRADED.',
          state: 'firing',
          startedAt: fixtureNow - 3_600_000,
          resolvedAt: null,
          ruleName: 'Storage health',
          acknowledgedBy: '',
          acknowledgedAt: null,
        },
        {
          id: 1,
          kind: 'zfs.pool.capacity',
          subject: 'tank',
          severity: 'warning',
          message: 'Pool tank is 81% full.',
          state: 'resolved',
          startedAt: fixtureNow - 7_200_000,
          resolvedAt: fixtureNow - 3_600_000,
          ruleName: 'Storage health',
          acknowledgedBy: '',
          acknowledgedAt: null,
        },
      ],
    });
    renderApp(base);
    const alerts = await screen.findByRole('region', { name: 'Storage alerts' });
    expect(within(alerts).getByText('Pool backup is DEGRADED.')).toBeVisible();
    expect(within(alerts).queryByText('Pool tank is 81% full.')).toBeNull();
  });

  it('shows disks and filesystems alone on a machine without ZFS', async () => {
    withStorage({
      ...nasStorage,
      zfs: { installed: false },
      pools: [],
      datasets: [],
      arc: undefined,
      smartAvailable: false,
      disks: [],
    });
    renderApp(base);
    expect(await screen.findByRole('region', { name: 'Physical disks' })).toBeVisible();
    expect(screen.queryByRole('region', { name: 'Pools' })).toBeNull();
    expect(screen.getByText(/Install smartmontools/)).toBeVisible();
  });

  it('explains ZFS that is installed but unreadable', async () => {
    withStorage({
      ...nasStorage,
      zfs: { installed: true, problem: 'The ZFS modules are not loaded.' },
      pools: [],
      datasets: [],
    });
    renderApp(base);
    expect(
      await screen.findByText(/couldn’t be read: The ZFS modules are not loaded./),
    ).toBeVisible();
  });

  it('says why there is nothing to show for an offline machine or a refusal', async () => {
    withStorage(nasStorage, { alerts: [] }, { ...nasDetails, online: false });
    renderApp(base);
    expect(await screen.findByText(/The machine is offline/)).toBeVisible();
  });

  it('shows the agent’s refusal', async () => {
    withStorage(null);
    renderApp(base);
    expect(await screen.findByText('The storage view is disabled.')).toBeVisible();
  });
});
