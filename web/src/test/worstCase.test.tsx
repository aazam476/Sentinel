// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type { AuditEntriesResponse } from '../api/types.gen';
import { findAccessibilityViolations } from './axe';
import {
  longDisplayName,
  longHostname,
  longUsername,
  worstAgents,
  worstAuditEntries,
  worstContainers,
  worstLogs,
  worstMachine,
  worstProcesses,
  worstServices,
  worstTokens,
  worstPasskeys,
  worstSession,
  worstSessions,
  worstStacks,
  worstStorage,
  worstScripts,
  worstBatch,
  worstRunOutput,
  worstSchedules,
  worstChannels,
  worstFleetAlerts,
  worstRules,
  worstFolder,
  worstFolderLink,
} from './fixtures/worst-case/fixtures';
import { renderApp } from './renderApp';
import { apiPath, json, server, signedInAs } from './server';

/**
 * These render every data-showing screen with the worst-case fixtures:
 * longest names, unbreakable strings, right-to-left text, emoji, empty
 * values, and 1,200 audit entries. jsdom can't measure layout, so these
 * check that nothing crashes, nothing is lost, and anything cut short has a
 * tooltip; the Playwright suite checks the same data for overflow in a real
 * browser at phone and desktop widths.
 */
describe('worst-case data', () => {
  it('shell: a 64-character username is shown in full in a tooltip', async () => {
    signedInAs(worstSession);
    renderApp('/');
    const header = await screen.findByRole('banner');
    expect(within(header).getByTitle(longUsername)).toHaveTextContent(longUsername);
    expect(screen.getByRole('heading', { name: 'Machines' })).toBeVisible();
  });

  it('account: long, right-to-left and one-letter passkey names, odd sessions', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath('auth/passkeys'), () => json({ passkeys: worstPasskeys })),
      http.get(apiPath('auth/sessions'), () => json({ sessions: worstSessions })),
    );
    const { container } = renderApp('/account');

    for (const passkey of worstPasskeys) {
      expect(await screen.findByText(passkey.name)).toBeInTheDocument();
    }
    expect(screen.getByText(/2001:0db8:85a3:0000:0000:8a2e:0370:7334/)).toBeInTheDocument();
    expect(screen.getAllByText(/Unknown browser/)).toHaveLength(3);
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('account: no passkeys at all', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath('auth/passkeys'), () => json({ passkeys: [] })),
      http.get(apiPath('auth/sessions'), () => json({ sessions: worstSessions.slice(0, 1) })),
    );
    renderApp('/account');
    expect(await screen.findByText('No passkeys yet.')).toBeVisible();
  });

  it('audit log: 1,200 entries render only what is on screen, extremes first', async () => {
    signedInAs(worstSession);
    const entries = worstAuditEntries();
    const page: AuditEntriesResponse = { entries, nextBefore: null };
    server.use(http.get(apiPath('audit'), () => json(page)));
    renderApp('/audit');

    const rows = await screen.findAllByRole('article');
    expect(rows.length).toBeGreaterThan(0);
    expect(rows.length).toBeLessThan(50);
    expect(rows[0]).toHaveTextContent("' OR '1'='1");
    expect(rows[1]).toHaveTextContent('ünïcødé-用户-👩‍💻');
    expect(rows[2]).toHaveTextContent('No user');
  });

  it('audit log: an empty log', async () => {
    signedInAs(worstSession);
    server.use(http.get(apiPath('audit'), () => json({ entries: [], nextBefore: null })));
    renderApp('/audit');
    expect(await screen.findByText('No entries yet.')).toBeVisible();
  });

  it('fleet: 1,200 machines with the longest names, tags and odd readings', async () => {
    signedInAs(worstSession);
    server.use(http.get(apiPath('agents'), () => json({ agents: worstAgents() })));
    const { container } = renderApp('/');

    expect(await screen.findByText('1,200 machines · 1,198 online')).toBeVisible();
    expect(screen.getAllByTitle(longDisplayName).length).toBeGreaterThan(0);
    expect(screen.getByText('Offline · last seen never')).toBeVisible();
    expect(screen.getByText('Waiting for the first reading…')).toBeVisible();
    expect(screen.getAllByText('Up 3650 d 0 h').length).toBeGreaterThan(0);
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('machine: every capability disabled, 256 long mount points, huge numbers', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath(`agents/${worstMachine.id}`), () => json(worstMachine)),
      http.get(apiPath(`agents/${worstMachine.id}/metrics`), () =>
        json({ metric: 'cpu.percent', resolution: 'raw', points: [] }),
      ),
    );
    const { container } = renderApp(`/machines/${worstMachine.id}`);

    expect(await screen.findByRole('heading', { name: longDisplayName })).toBeVisible();
    expect(screen.getByText(longHostname)).toBeVisible();
    expect(screen.getAllByText('Disabled by local policy')).toHaveLength(8);
    expect(screen.getByText('an-unknown-capability-from-a-newer-agent')).toBeVisible();
    expect(screen.getByText('2 PB')).toBeVisible();
    const mounts = within(screen.getByRole('region', { name: 'Filesystems' })).getAllByRole(
      'listitem',
    );
    expect(mounts).toHaveLength(256);
    expect(mounts[0]?.querySelector('[title]')).not.toBeNull();
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('services: escaped, right-to-left and one-letter names, cut descriptions, 1,205 rows', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath(`agents/${worstMachine.id}`), () => json(worstMachine)),
      http.get(apiPath(`agents/${worstMachine.id}/services`), () => json(worstServices())),
    );
    const { container } = renderApp(`/machines/${worstMachine.id}/services`);

    expect(await screen.findByText('1,205 services')).toBeVisible();
    const first = worstServices().services[0];
    expect(screen.getByText(first?.name ?? '')).toBeVisible();
    expect(screen.getByTitle(first?.description ?? '')).toBeVisible();
    expect(screen.getByText('خدمة-النسخ-الاحتياطي.service')).toBeVisible();
    // Virtualized: far fewer rows in the page than services.
    expect(screen.getAllByRole('article').length).toBeLessThan(50);
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('processes: a 4 KB command line, 3,187% CPU, 1.5 TB, a nameless process', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath(`agents/${worstMachine.id}`), () => json(worstMachine)),
      http.get(apiPath(`agents/${worstMachine.id}/processes`), () => json(worstProcesses())),
    );
    const { container } = renderApp(`/machines/${worstMachine.id}/processes`);

    expect(await screen.findByText('1,204 processes')).toBeVisible();
    const java = screen.getByRole('article', { name: 'java (2147483647)' });
    expect(within(java).getByText('CPU 3187.5%')).toBeVisible();
    expect(within(java).getByText('1.5 TB')).toBeVisible();
    expect(within(java).getByTitle(/^\/usr\/lib\/jvm/)).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('logs: an unbreakable blob, a stack trace, no source, the newest 1,000 of more', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath(`agents/${worstMachine.id}`), () => json(worstMachine)),
      http.get(apiPath(`agents/${worstMachine.id}/logs`), () => json(worstLogs())),
    );
    const { container } = renderApp(`/machines/${worstMachine.id}/logs`);

    expect(await screen.findByText(/^The newest 1,000 entries;/)).toBeVisible();
    expect(screen.getByText(/^certificate rejected: MIIB/)).toBeVisible();
    expect(screen.getByText(/ValueError: 💥/)).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('docker: compose’s longest names, a 64-character digest, 1,203 containers, odd stacks', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath(`agents/${worstMachine.id}`), () =>
        json({ ...worstMachine, capabilities: [] }),
      ),
      http.get(apiPath(`agents/${worstMachine.id}/docker/containers`), () =>
        json(worstContainers()),
      ),
      http.get(apiPath(`agents/${worstMachine.id}/docker/stacks`), () => json(worstStacks)),
    );
    const { container } = renderApp(`/machines/${worstMachine.id}/docker`);
    expect(await screen.findByText('1,203 containers · Podman 5.4.2')).toBeVisible();
    const longest = worstContainers().containers[0];
    expect(screen.getByTitle(new RegExp(`^${(longest?.image ?? '').slice(0, 40)}`))).toBeVisible();
    expect(screen.getAllByRole('article').length).toBeLessThan(50);
    expect(await findAccessibilityViolations(container)).toEqual([]);

    const { container: stacksView } = renderApp(`/machines/${worstMachine.id}/docker/stacks`);
    expect(await within(stacksView).findByText('120 of 124 containers running')).toBeVisible();
    expect(within(stacksView).getByText('Not running')).toBeVisible();
  });

  it('storage: a FAULTED pool’s long status, petabytes, 1,503 datasets, a 24-wide dRAID', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath(`agents/${worstMachine.id}`), () =>
        json({ ...worstMachine, capabilities: [] }),
      ),
      http.get(apiPath(`agents/${worstMachine.id}/storage`), () => json(worstStorage())),
      http.get(apiPath(`agents/${worstMachine.id}/alerts`), () => json({ alerts: [] })),
    );
    const user = userEvent.setup();
    const { container } = renderApp(`/machines/${worstMachine.id}/storage`);
    const pool = await screen.findByRole('article', { name: /^Pool northwind-/ });
    expect(within(pool).getByText('FAULTED')).toBeVisible();
    expect(within(pool).getAllByText('5 PB')).toHaveLength(2);
    expect(screen.getByText(/4,294,967,295 checksum$/)).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);

    // Docker's ZFS driver makes thousands of datasets; 200 show at first.
    const datasets = screen.getByRole('region', { name: 'Datasets' });
    const rows = () => datasets.querySelectorAll('li').length;
    expect(rows()).toBe(200);
    await user.click(within(datasets).getByRole('button', { name: 'Show all 1,503 datasets' }));
    expect(rows()).toBe(1503);
    // Rendering 1,503 rows takes jsdom a few seconds, more on a busy CI runner.
  }, 30_000);

  it('files: 5,000 entries, 255-character names, a 999 TB file, only some rendered', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath(`agents/${worstMachine.id}`), () =>
        json({ ...worstMachine, capabilities: [] }),
      ),
      http.get(apiPath(`agents/${worstMachine.id}/files`), () => json(worstFolder())),
    );
    const { container } = renderApp(worstFolderLink);
    expect(await screen.findByText(/5,000 items · only the first 5,000 are shown/)).toBeVisible();
    expect(screen.getAllByRole('listitem').length).toBeLessThan(100);
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('scripts: 300 scripts, a batch on 1,000 machines, a 20,000-character line of output', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath('agents'), () => json({ agents: worstAgents() })),
      http.get(apiPath('scripts'), () => json(worstScripts())),
      http.get(apiPath('script-batches/3'), () => json(worstBatch())),
      http.get(apiPath('script-runs/101'), () => json(worstRunOutput())),
      http.get(apiPath('schedules'), () => json(worstSchedules)),
    );
    const user = userEvent.setup();
    const { container } = renderApp('/scripts/runs/3');
    const first = await screen.findByRole('listitem', { name: longDisplayName });
    expect(within(first).getByText('Running')).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);
    const second = screen.getAllByRole('listitem')[1];
    if (second === undefined) {
      throw new Error('no second run');
    }
    await user.click(within(second).getAllByRole('button')[0] as HTMLElement);
    expect(
      await within(second).findByText('Only the first 1 MB of output was kept.'),
    ).toBeVisible();

    const { container: schedules } = renderApp('/scripts/schedules');
    const schedule = await within(schedules).findByRole('listitem', {
      name: /^Schedule of Nightly/,
    });
    expect(within(schedule).getByText('Paused')).toBeVisible();
    expect(within(schedule).getByText('1,000 machines')).toBeVisible();
  }, 30_000);

  it('alerts: 500 alerts, every rule type notifying 20 channels, email to 50 people', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath('agents'), () => json({ agents: worstAgents() })),
      http.get(apiPath('alerts'), () => json(worstFleetAlerts())),
      http.get(apiPath('alert-rules'), () => json(worstRules)),
      http.get(apiPath('notification-channels'), () => json(worstChannels)),
    );
    const { container } = renderApp('/alerts');
    const firing = await screen.findByRole('list', { name: 'Firing alerts' });
    expect(within(firing).getAllByRole('listitem')).toHaveLength(300);
    expect(
      within(firing).getAllByText(`Acknowledged by ${longUsername}`, { exact: false }).length,
    ).toBe(75);
    expect(await findAccessibilityViolations(container)).toEqual([]);

    const { container: rules } = renderApp('/alerts/rules');
    const rule = (await within(rules).findAllByRole('listitem'))[0];
    expect(rule).toHaveTextContent('Temperature below 999.5 °C for 1440 minutes');
    expect(rule).toHaveTextContent(/Notifies channel-01.*channel-19, Ops team/);

    const { container: channels } = renderApp('/alerts/channels');
    expect(await within(channels).findAllByText(/^Last send failed: /)).toHaveLength(5);
  }, 30_000);

  it('add machine: a 100-character note and a 1,000-use token', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath('enrollment-tokens'), () =>
        json({ tokens: worstTokens, serverFingerprint: 'SHA256:x', tlsFingerprint: '' }),
      ),
    );
    const { container } = renderApp('/machines/add');
    expect(await screen.findByText(/used 999 of 1000/)).toBeVisible();
    expect(screen.getByText('Command #2')).toBeVisible();
    expect(screen.getByText(/Expired · used 0 of 1/)).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });
});
