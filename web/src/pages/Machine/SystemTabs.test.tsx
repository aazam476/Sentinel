// SPDX-License-Identifier: Apache-2.0
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';

import type { AgentDetails } from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { eventStream, useFakeEventSource } from '../../test/fakeEventSource';
import {
  adminSession,
  nasDetails,
  nasLogs,
  nasProcesses,
  nasServices,
  viewerSession,
} from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const base = `/machines/${nasDetails.id}`;

/** withMachine serves the machine, which can be swapped to change its status. */
function withMachine(details: AgentDetails = nasDetails) {
  let current = details;
  server.use(
    http.get(apiPath(`agents/${details.id}`), () => json(current)),
    http.get(apiPath(`agents/${details.id}/metrics`), () =>
      json({ metric: 'cpu', resolution: 'raw', points: [] }),
    ),
  );
  return {
    set: (next: AgentDetails) => {
      current = next;
    },
  };
}

/** recordPosts records the bodies and paths of POSTs to the machine. */
function recordPosts(status = 204) {
  const posts: { path: string; body: unknown }[] = [];
  server.use(
    http.post(apiPath(`agents/${nasDetails.id}/*`), async ({ request }) => {
      const text = await request.text();
      posts.push({
        path: new URL(request.url).pathname,
        body: text === '' ? undefined : (JSON.parse(text) as unknown),
      });
      return new HttpResponse(null, { status });
    }),
  );
  return posts;
}

const tabs = () =>
  within(screen.getByRole('navigation', { name: 'Machine sections' }))
    .getAllByRole('link')
    .map((link) => link.textContent);

describe('machine tabs', () => {
  it('offers operators every section and power actions', async () => {
    signedInAs(adminSession);
    withMachine();
    renderApp(base);
    await screen.findByRole('heading', { name: nasDetails.name });
    expect(tabs()).toEqual([
      'Overview',
      'Terminal',
      'Services',
      'Processes',
      'Logs',
      'Storage',
      'Docker',
    ]);
    expect(screen.getByRole('button', { name: 'Reboot' })).toBeEnabled();
  });

  it('shows viewers only what they may see, and no power actions', async () => {
    signedInAs(viewerSession);
    withMachine();
    renderApp(base);
    await screen.findByRole('heading', { name: nasDetails.name });
    expect(tabs()).toEqual(['Overview', 'Services', 'Logs', 'Storage', 'Docker']);
    expect(screen.queryByRole('button', { name: 'Reboot' })).not.toBeInTheDocument();
  });

  it('SEC-22: hides the terminal and power actions a local policy switches off', async () => {
    signedInAs(adminSession);
    withMachine({
      ...nasDetails,
      capabilities: [
        { name: 'terminal', enabled: false, reason: 'Disabled by local policy' },
        { name: 'power', enabled: false, reason: 'Disabled by local policy' },
      ],
    });
    renderApp(base);
    await screen.findByRole('heading', { name: nasDetails.name });
    expect(tabs()).not.toContain('Terminal');
    expect(screen.queryByRole('button', { name: 'Reboot' })).not.toBeInTheDocument();
  });
});

describe('services', () => {
  it('lists services, narrows them, and restarts one', async () => {
    signedInAs(adminSession);
    withMachine();
    server.use(http.get(apiPath(`agents/${nasDetails.id}/services`), () => json(nasServices)));
    const posts = recordPosts();
    const user = userEvent.setup();
    const { container } = renderApp(`${base}/services`);

    expect(await screen.findByText('4 services')).toBeVisible();
    const nginx = screen.getByRole('article', { name: 'nginx.service' });
    expect(within(nginx).getByText('Running')).toBeVisible();
    expect(within(nginx).getByText('Starts at boot')).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.selectOptions(screen.getByRole('combobox', { name: 'Show' }), 'failed');
    expect(screen.getByText('1 of 4 services')).toBeVisible();
    await user.selectOptions(screen.getByRole('combobox', { name: 'Show' }), 'all');
    await user.type(screen.getByRole('searchbox', { name: 'Search services' }), 'D-Bus');
    expect(await screen.findByText('1 of 4 services')).toBeVisible();
    await user.clear(screen.getByRole('searchbox', { name: 'Search services' }));

    await user.click(await screen.findByRole('button', { name: 'Actions for nginx.service' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Restart' }));
    expect(await screen.findByText('Restart: nginx.service')).toBeVisible();
    expect(posts.map((post) => post.path)).toEqual([
      `/api/agents/${nasDetails.id}/services/nginx.service/restart`,
    ]);
  });

  it('asks before stopping a service, and offers only actions that apply', async () => {
    signedInAs(adminSession);
    withMachine();
    server.use(http.get(apiPath(`agents/${nasDetails.id}/services`), () => json(nasServices)));
    const posts = recordPosts();
    const user = userEvent.setup();
    renderApp(`${base}/services`);

    await user.click(await screen.findByRole('button', { name: 'Actions for dbus.service' }));
    const items = (await screen.findAllByRole('menuitem')).map((item) => item.textContent);
    expect(items).toEqual(['Restart', 'Stop', 'View logs']); // started by the system: no enable/disable
    await user.click(screen.getByRole('menuitem', { name: 'Stop' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Stop dbus.service?' });
    await user.click(within(dialog).getByRole('button', { name: 'Stop' }));
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
  });

  it('shows viewers the list without actions, and takes them to a service’s logs', async () => {
    signedInAs(viewerSession);
    withMachine();
    server.use(
      http.get(apiPath(`agents/${nasDetails.id}/services`), () => json(nasServices)),
      http.get(apiPath(`agents/${nasDetails.id}/logs`), () => json(nasLogs)),
    );
    const user = userEvent.setup();
    const { router } = renderApp(`${base}/services`);

    await user.click(await screen.findByRole('button', { name: 'Actions for nginx.service' }));
    expect((await screen.findAllByRole('menuitem')).map((item) => item.textContent)).toEqual([
      'View logs',
    ]);
    await user.click(screen.getByRole('menuitem', { name: 'View logs' }));
    await waitFor(() => {
      expect(router.state.location.search).toBe('?service=nginx.service');
    });
    expect(await screen.findByRole('searchbox', { name: 'Service' })).toHaveValue('nginx.service');
  });

  it('reports a machine whose service manager isn’t supported', async () => {
    signedInAs(adminSession);
    withMachine();
    server.use(
      http.get(apiPath(`agents/${nasDetails.id}/services`), () =>
        apiError(501, 'unsupported', 'this machine’s service manager isn’t supported'),
      ),
    );
    renderApp(`${base}/services`);
    expect(
      await screen.findByText('This machine’s service manager isn’t supported.'),
    ).toBeVisible();
  });
});

describe('processes', () => {
  it('lists the busiest first and ends one after asking', async () => {
    signedInAs(adminSession);
    withMachine();
    server.use(http.get(apiPath(`agents/${nasDetails.id}/processes`), () => json(nasProcesses)));
    const posts = recordPosts();
    const user = userEvent.setup();
    const { container } = renderApp(`${base}/processes`);

    expect(await screen.findByText('3 processes')).toBeVisible();
    const [busiest] = screen.getAllByRole('article');
    expect(busiest).toHaveAccessibleName('ffmpeg (4242)');
    expect(
      within(screen.getByRole('article', { name: 'ffmpeg (4242)' })).getByText('CPU 187.3%'),
    ).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.selectOptions(screen.getByRole('combobox', { name: 'Sort by' }), 'name');
    expect(screen.getAllByRole('article')[0]).toHaveAccessibleName('ffmpeg (4242)');
    await user.type(screen.getByRole('searchbox', { name: 'Search processes' }), 'postgres');
    expect(screen.getByText('1 of 3 processes')).toBeVisible();

    await user.click(screen.getByRole('button', { name: 'Actions for postgres (900)' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Kill process' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'End postgres (900)?' });
    await user.click(within(dialog).getByRole('button', { name: 'Kill' }));
    expect(await screen.findByText('Ended postgres (900)')).toBeVisible();
    expect(posts).toEqual([
      { path: `/api/agents/${nasDetails.id}/processes/900/kill`, body: { force: true } },
    ]);
  });

  it('says so when the machine is offline', async () => {
    signedInAs(adminSession);
    withMachine({ ...nasDetails, online: false });
    renderApp(`${base}/processes`);
    expect(await screen.findByText(/The machine is offline/)).toBeVisible();
  });
});

describe('logs', () => {
  it('reads the last hour, and filters by level and range', async () => {
    signedInAs(viewerSession);
    withMachine();
    const queries: URLSearchParams[] = [];
    server.use(
      http.get(apiPath(`agents/${nasDetails.id}/logs`), ({ request }) => {
        queries.push(new URL(request.url).searchParams);
        return json(nasLogs);
      }),
    );
    const user = userEvent.setup();
    const { container } = renderApp(`${base}/logs`);

    expect(
      await screen.findByText('bind() to 0.0.0.0:80 failed (98: Address already in use)'),
    ).toBeVisible();
    expect(screen.getByText('3 entries')).toBeVisible();
    expect(queries[0]?.get('limit')).toBe('1000');
    expect(queries[0]?.has('level')).toBe(false);
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.selectOptions(screen.getByRole('combobox', { name: 'Level' }), 'error');
    await waitFor(() => {
      expect(queries.at(-1)?.get('level')).toBe('error');
    });
    await user.click(screen.getByRole('button', { name: '24 h' }));
    await waitFor(() => {
      const since = Number(queries.at(-1)?.get('since'));
      expect(Date.now() - since).toBeGreaterThanOrEqual(24 * 60 * 60_000);
    });
    await user.type(screen.getByRole('searchbox', { name: 'Service' }), 'nginx.service{Enter}');
    await waitFor(() => {
      expect(queries.at(-1)?.get('service')).toBe('nginx.service');
    });
  });

  it('says when there is nothing, and when there was more than it shows', async () => {
    signedInAs(viewerSession);
    withMachine();
    let answer = { entries: [], truncated: false };
    server.use(http.get(apiPath(`agents/${nasDetails.id}/logs`), () => json(answer)));
    const user = userEvent.setup();
    renderApp(`${base}/logs`);
    expect(await screen.findByText('No log entries in this time range.')).toBeVisible();

    answer = { ...nasLogs, truncated: true } as never;
    await user.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(await screen.findByText(/^The newest 3 entries;/)).toBeVisible();
  });
});

describe('power', () => {
  it('reboots after asking, then waits for the machine to come back', async () => {
    useFakeEventSource();
    signedInAs(adminSession);
    const machine = withMachine();
    const posts = recordPosts(202);
    const user = userEvent.setup();
    renderApp(base);

    await user.click(await screen.findByRole('button', { name: 'Reboot' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Reboot this machine?' });
    await user.click(within(dialog).getByRole('button', { name: 'Reboot' }));
    expect(
      await screen.findByText('Rebooting. Waiting for the machine to come back…'),
    ).toBeVisible();
    expect(posts).toEqual([
      { path: `/api/agents/${nasDetails.id}/power`, body: { action: 'reboot' } },
    ]);

    machine.set({ ...nasDetails, online: false });
    eventStream().emit('agent', { agentId: nasDetails.id, kind: 'offline' });
    expect(await screen.findByText('Offline')).toBeVisible();
    expect(screen.getByText('Rebooting. Waiting for the machine to come back…')).toBeVisible();

    machine.set(nasDetails);
    eventStream().emit('agent', { agentId: nasDetails.id, kind: 'online' });
    expect(await screen.findByText(`${nasDetails.name} is back online`)).toBeVisible();
    expect(screen.getByRole('button', { name: 'Reboot' })).toBeEnabled();
  });

  it('shows the machine’s refusal', async () => {
    signedInAs(adminSession);
    withMachine();
    server.use(
      http.post(apiPath(`agents/${nasDetails.id}/power`), () =>
        apiError(
          403,
          'policy_denied',
          'Rebooting and shutting down are disabled by this machine’s local policy.',
        ),
      ),
    );
    const user = userEvent.setup();
    renderApp(base);
    await user.click(await screen.findByRole('button', { name: 'Shut down' }));
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Shut down' }),
    );
    expect(
      await screen.findByText(
        'Rebooting and shutting down are disabled by this machine’s local policy.',
      ),
    ).toBeVisible();
  });
});
