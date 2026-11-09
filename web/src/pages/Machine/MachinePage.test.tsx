// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type {
  AgentDetails,
  MachineAlerts,
  MetricSeries,
  UpdateAgentRequest,
} from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { eventStream, useFakeEventSource } from '../../test/fakeEventSource';
import {
  adminSession,
  fixtureNow,
  nasDetails,
  viewerSession,
} from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const path = `/machines/${nasDetails.id}`;

/** withMachine serves the NAS and a short series for any metric query. */
function withMachine(details: AgentDetails = nasDetails) {
  const queries: URLSearchParams[] = [];
  server.use(
    http.get(apiPath(`agents/${details.id}`), () => json(details)),
    http.get(apiPath(`agents/${details.id}/alerts`), () => json({ alerts: [] })),
    http.get(apiPath(`agents/${details.id}/metrics`), ({ request }) => {
      const query = new URL(request.url).searchParams;
      queries.push(query);
      const series: MetricSeries = {
        metric: query.get('metric') ?? '',
        resolution: 'raw',
        points: [
          { time: fixtureNow - 20_000, value: 10, min: 10, max: 10 },
          { time: fixtureNow - 10_000, value: 20, min: 20, max: 20 },
        ],
      };
      return json(series);
    }),
  );
  return queries;
}

describe('machine view', () => {
  it('SEC-22: shows the machine’s details and which capabilities its local policy disables', async () => {
    signedInAs(adminSession);
    withMachine();
    const { container } = renderApp(path);

    expect(await screen.findByRole('heading', { name: 'Living room NAS' })).toBeVisible();
    expect(screen.getByText('Intel(R) N100 (4 cores)')).toBeVisible();
    expect(screen.getByText('16 GB')).toBeVisible();
    const capabilities = screen.getByRole('region', { name: 'Capabilities' });
    expect(within(capabilities).getByText('Disabled by local policy')).toBeVisible();
    expect(within(capabilities).getAllByText('Allowed')).toHaveLength(2);
    expect(within(capabilities).getByText('Remote desktop')).toBeVisible();

    const filesystems = within(screen.getByRole('region', { name: 'Filesystems' })).getAllByRole(
      'listitem',
    );
    expect(filesystems[0]).toHaveTextContent('/srv'); // the fullest first
    expect(filesystems[0]).toHaveTextContent('9.2 TB of 10 TB · 92%');
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('starts the live charts from recent history and adds samples as they arrive', async () => {
    useFakeEventSource();
    signedInAs(adminSession);
    withMachine();
    renderApp(path);
    const [cpu] = await screen.findAllByTestId('live-chart');
    await vi.waitFor(() => {
      expect(cpu).toHaveAttribute('data-points', '10,20,23');
    });

    eventStream().emit('metrics', {
      agentId: nasDetails.id,
      current: {
        time: fixtureNow + 10_000,
        uptimeSeconds: 1,
        values: { 'cpu.percent': 77, 'memory.percent': 50 },
      },
    });
    await vi.waitFor(() => {
      expect(cpu).toHaveAttribute('data-points', '10,20,23,77');
    });
    expect(cpu?.closest('figure')).toHaveTextContent('CPU77%');
  });

  it('charts history for the chosen metric and range', async () => {
    signedInAs(adminSession);
    const queries = withMachine();
    renderApp(path);
    const user = userEvent.setup();
    const history = await screen.findByRole('region', { name: 'History' });

    await user.selectOptions(within(history).getByLabelText('Metric'), 'Network in');
    await user.click(within(history).getByRole('button', { name: '7 d' }));
    const last = queries.at(-1);
    expect(last?.get('metric')).toBe('network.received');
    expect(Number(last?.get('to')) - Number(last?.get('from'))).toBe(7 * 86_400_000);
    expect(within(history).getByRole('button', { name: '7 d' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(within(history).getByRole('img', { name: 'Network in over 7 d' })).toBeVisible();
  });

  it('lets an admin rename and re-tag the machine', async () => {
    signedInAs(adminSession);
    withMachine();
    let sent: UpdateAgentRequest | undefined;
    server.use(
      http.patch(apiPath(`agents/${nasDetails.id}`), async ({ request }) => {
        sent = (await request.json()) as UpdateAgentRequest;
        return json({ ...nasDetails, name: 'NAS', displayName: 'NAS', tags: ['a', 'b'] });
      }),
      http.get(apiPath('agents'), () => json({ agents: [] })),
    );
    renderApp(path);
    const user = userEvent.setup();
    const name = await screen.findByLabelText('Display name');
    await user.clear(name);
    await user.type(name, 'NAS');
    const tags = screen.getByLabelText('Tags, separated by commas');
    await user.clear(tags);
    await user.type(tags, 'a, b  ,');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByText('Saved')).toBeVisible();
    expect(sent).toEqual({ displayName: 'NAS', tags: ['a', 'b'] });
    expect(screen.getByRole('heading', { name: 'NAS' })).toBeVisible();
  });

  it('revokes the machine after confirmation and returns to the fleet', async () => {
    signedInAs(adminSession);
    withMachine();
    let revoked = false;
    server.use(
      http.post(apiPath(`agents/${nasDetails.id}/revoke`), () => {
        revoked = true;
        return new Response(null, { status: 204 });
      }),
    );
    renderApp(path);
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Revoke machine' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Revoke Living room NAS?' });
    await user.click(within(dialog).getByRole('button', { name: 'Revoke' }));

    expect(await screen.findByText('Revoked Living room NAS')).toBeVisible();
    expect(await screen.findByRole('heading', { name: 'Machines' })).toBeVisible();
    expect(revoked).toBe(true);
  });

  it('shows no settings to a viewer', async () => {
    signedInAs(viewerSession);
    withMachine();
    renderApp(path);
    await screen.findByRole('heading', { name: 'Living room NAS' });
    expect(screen.queryByRole('region', { name: 'Settings' })).not.toBeInTheDocument();
  });

  it('shows when an offline machine was last seen and that its filesystems need it online', async () => {
    signedInAs(adminSession);
    withMachine({
      ...nasDetails,
      online: false,
      current: null,
      filesystems: [],
      lastSeenAt: fixtureNow - 7_200_000,
    });
    renderApp(path);
    expect(await screen.findByText('Offline')).toBeVisible();
    expect(screen.getByText(/Last seen/)).toBeVisible();
    expect(screen.getByText('Shown while the machine is online.')).toBeVisible();
  });

  it('says when the machine doesn’t exist', async () => {
    signedInAs(adminSession);
    server.use(http.get(apiPath('agents/missing'), () => apiError(404, 'not_found', 'not found')));
    renderApp('/machines/missing');
    expect(await screen.findByText('This machine doesn’t exist or was revoked.')).toBeVisible();
  });

  it('lists the machine’s firing alerts, and refreshes them when one starts', async () => {
    useFakeEventSource();
    signedInAs(adminSession);
    withMachine();
    let alerts: MachineAlerts = { alerts: [] };
    server.use(http.get(apiPath(`agents/${nasDetails.id}/alerts`), () => json(alerts)));
    renderApp(path);
    await screen.findByRole('heading', { name: 'Living room NAS' });
    expect(screen.queryByRole('region', { name: 'Alerts' })).toBeNull();

    alerts = {
      alerts: [
        {
          id: 1,
          kind: 'rule.2',
          subject: '',
          severity: 'warning',
          message: 'CPU has been above 90% for 5 minutes (97%).',
          state: 'firing',
          startedAt: fixtureNow,
          resolvedAt: null,
          ruleName: 'CPU busy',
          acknowledgedBy: 'admin',
          acknowledgedAt: fixtureNow,
        },
      ],
    };
    eventStream().emit('agent', { agentId: nasDetails.id, kind: 'alerts' });
    const region = await screen.findByRole('region', { name: 'Alerts' });
    expect(within(region).getByText('CPU has been above 90% for 5 minutes (97%).')).toBeVisible();
    expect(within(region).getByText(/acknowledged by admin/)).toBeVisible();
  });

  it('lets an admin put the machine in maintenance, and take it out', async () => {
    signedInAs(adminSession);
    withMachine();
    const user = userEvent.setup();
    const sent: (number | null)[] = [];
    server.use(
      http.put(apiPath(`agents/${nasDetails.id}/maintenance`), async ({ request }) => {
        const { until } = (await request.json()) as { until: number | null };
        sent.push(until);
        return json({ ...nasDetails, maintenanceUntil: until });
      }),
    );
    renderApp(path);
    const panel = await screen.findByRole('region', { name: 'Maintenance' });
    await user.selectOptions(within(panel).getByRole('combobox', { name: 'For' }), '1 day');
    await user.click(within(panel).getByRole('button', { name: 'Start maintenance' }));
    expect(await within(panel).findByText(/In maintenance until/)).toBeVisible();
    expect(screen.getByText('In maintenance')).toBeVisible();
    const until = sent[0] ?? 0;
    expect(until - Date.now()).toBeGreaterThan(23 * 3_600_000);
    expect(until - Date.now()).toBeLessThanOrEqual(24 * 3_600_000);

    await user.click(within(panel).getByRole('button', { name: 'End maintenance' }));
    expect(await within(panel).findByRole('button', { name: 'Start maintenance' })).toBeVisible();
    expect(sent[1]).toBeNull();
  }, 15_000);

  it('shows viewers no maintenance control', async () => {
    signedInAs(viewerSession);
    withMachine();
    renderApp(path);
    await screen.findByRole('heading', { name: 'Living room NAS' });
    expect(screen.queryByRole('region', { name: 'Maintenance' })).toBeNull();
  });
});
