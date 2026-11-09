// SPDX-License-Identifier: Apache-2.0
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';

import type { AlertRule, FleetAlert, NotificationChannel } from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import {
  adminSession,
  agents,
  fixtureNow,
  viewerSession,
} from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';
import { describeCondition, describeTarget } from './alertText';

const nas = agents[0];

const degraded: FleetAlert = {
  id: 1,
  agentId: nas?.id ?? '',
  agentName: 'Living room NAS',
  kind: 'zfs.pool.health',
  subject: 'tank',
  severity: 'critical',
  message: 'Pool tank is FAULTED.',
  state: 'firing',
  startedAt: fixtureNow - 3_600_000,
  resolvedAt: null,
  ruleName: 'Storage health',
  acknowledgedBy: '',
  acknowledgedAt: null,
};

const resolvedCPU: FleetAlert = {
  ...degraded,
  id: 2,
  kind: 'rule.2',
  subject: '',
  severity: 'warning',
  message: 'CPU has been above 90% for 5 minutes (97%).',
  state: 'resolved',
  resolvedAt: fixtureNow - 60_000,
  ruleName: 'CPU busy',
};

const opsHook: NotificationChannel = {
  id: 4,
  type: 'webhook',
  name: 'Ops',
  enabled: true,
  config: {},
  summary: 'posts to hooks.example.com',
  hasSecret: true,
  lastSentAt: fixtureNow,
  lastError: '',
};

const storageRule: AlertRule = {
  id: 1,
  type: 'storage',
  name: 'Storage health',
  config: { capacityPercent: 80 },
  target: { kind: 'all', value: '' },
  enabled: true,
  channelIds: [4],
};

function withAlerts(session = adminSession) {
  signedInAs(session);
  const sent: string[] = [];
  let alerts = [degraded, resolvedCPU];
  server.use(
    http.get(apiPath('agents'), () => json({ agents })),
    http.get(apiPath('alerts'), () => json({ alerts })),
    http.post(apiPath('alerts/1/acknowledge'), () => {
      sent.push('acknowledge 1');
      alerts = [{ ...degraded, acknowledgedBy: 'admin', acknowledgedAt: fixtureNow }, resolvedCPU];
      return new HttpResponse(null, { status: 204 });
    }),
    http.get(apiPath('alert-rules'), () => json({ rules: [storageRule] })),
    http.get(apiPath('notification-channels'), () => json({ channels: [opsHook] })),
  );
  return sent;
}

describe('Alerts', () => {
  it('lists firing alerts, then resolved ones, and acknowledges one', async () => {
    const sent = withAlerts();
    const user = userEvent.setup();
    const { container } = renderApp('/alerts');
    const firing = await screen.findByRole('list', { name: 'Firing alerts' });
    const alert = within(firing).getByRole('listitem', { name: 'Pool tank is FAULTED.' });
    expect(within(alert).getByText('Critical')).toBeVisible();
    expect(within(alert).getByRole('link', { name: 'Living room NAS' })).toHaveAttribute(
      'href',
      `/machines/${nas?.id ?? ''}`,
    );
    const resolved = screen.getByRole('list', { name: 'Resolved alerts' });
    expect(within(resolved).getByText('CPU has been above 90% for 5 minutes (97%).')).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.click(within(alert).getByRole('button', { name: 'Acknowledge' }));
    expect(await within(alert).findByText(/Acknowledged by admin/)).toBeVisible();
    expect(sent).toEqual(['acknowledge 1']);
  });

  it('shows viewers the alerts, without acknowledging or the rule and channel sections', async () => {
    withAlerts(viewerSession);
    renderApp('/alerts');
    const firing = await screen.findByRole('list', { name: 'Firing alerts' });
    expect(within(firing).queryByRole('button', { name: 'Acknowledge' })).toBeNull();
    expect(screen.queryByRole('navigation', { name: 'Alert sections' })).toBeNull();
    expect(screen.getByRole('link', { name: 'Alerts' })).toBeVisible();
  });

  it('says when nothing needs attention', async () => {
    withAlerts();
    server.use(http.get(apiPath('alerts'), () => json({ alerts: [] })));
    renderApp('/alerts');
    expect(await screen.findByText('Nothing needs attention.')).toBeVisible();
    expect(screen.getByText('No alerts have resolved yet.')).toBeVisible();
  });

  it('adds a CPU rule that notifies a channel, and shows the server’s objection', async () => {
    withAlerts();
    const user = userEvent.setup();
    const bodies: unknown[] = [];
    server.use(
      http.post(apiPath('alert-rules'), async ({ request }) => {
        const body = (await request.json()) as { config: { threshold: number } };
        bodies.push(body);
        if (body.config.threshold > 1000) {
          return apiError(400, 'invalid_input', 'choose a threshold from 0 to 1000', {
            field: 'threshold',
          });
        }
        return json({ ...storageRule, id: 2 });
      }),
    );
    const { container } = renderApp('/alerts/rules');
    const rule = await screen.findByRole('listitem', { name: 'Rule Storage health' });
    expect(within(rule).getByText('Notifies Ops')).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.click(screen.getByRole('button', { name: 'New rule' }));
    const dialog = await screen.findByRole('dialog', { name: 'New alert rule' });
    await user.type(within(dialog).getByRole('textbox', { name: 'Name' }), 'CPU busy');
    const threshold = within(dialog).getByRole('spinbutton', { name: 'Threshold' });
    await user.clear(threshold);
    await user.type(threshold, '5000');
    expect(within(dialog).getByText('CPU above 5000% for 5 minutes')).toBeVisible();
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(await within(dialog).findByText('Choose a threshold from 0 to 1000.')).toBeVisible();

    await user.clear(threshold);
    await user.type(threshold, '90');
    await user.selectOptions(within(dialog).getByRole('combobox', { name: 'On' }), 'tag');
    await user.type(within(dialog).getByRole('textbox', { name: 'Tag' }), 'homelab');
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(bodies[1]).toEqual({
      type: 'metric',
      name: 'CPU busy',
      config: { metric: 'cpu.percent', direction: 'above', threshold: 90, minutes: 5 },
      target: { kind: 'tag', value: 'homelab' },
      enabled: true,
      channelIds: [4],
    });
  }, 20_000);

  it('adds an email channel, sending its recipients as a list, and sends a test', async () => {
    withAlerts();
    const user = userEvent.setup();
    const sent: unknown[] = [];
    server.use(
      http.post(apiPath('notification-channels'), async ({ request }) => {
        sent.push(await request.json());
        return json({ ...opsHook, id: 5, type: 'email', name: 'Mail' });
      }),
      http.post(apiPath('notification-channels/4/test'), () =>
        apiError(502, 'channel_failed', 'The test notification wasn’t sent: 403 Forbidden'),
      ),
    );
    renderApp('/alerts/channels');
    const ops = await screen.findByRole('listitem', { name: 'Channel Ops' });
    expect(within(ops).getByText('posts to hooks.example.com')).toBeVisible();
    await user.click(within(ops).getByRole('button', { name: 'Send test' }));
    expect(
      await screen.findByText('The test notification wasn’t sent: 403 Forbidden.'),
    ).toBeVisible();

    await user.click(screen.getByRole('button', { name: 'New channel' }));
    const dialog = await screen.findByRole('dialog', { name: 'New channel' });
    await user.type(within(dialog).getByRole('textbox', { name: 'Name' }), 'Mail');
    await user.type(
      within(dialog).getByRole('textbox', { name: 'Mail server' }),
      'smtp.example.com',
    );
    await user.type(within(dialog).getByRole('textbox', { name: 'From' }), 'sentinel@example.com');
    await user.type(
      within(dialog).getByRole('textbox', { name: 'To' }),
      'ops@example.com, me@example.com',
    );
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(sent).toEqual([
        {
          type: 'email',
          name: 'Mail',
          enabled: true,
          config: {
            smtpHost: 'smtp.example.com',
            from: 'sentinel@example.com',
            to: ['ops@example.com', 'me@example.com'],
          },
        },
      ]);
    });
  }, 20_000);

  it('keeps a channel’s stored secret when a change leaves it empty', async () => {
    withAlerts();
    const user = userEvent.setup();
    const sent: unknown[] = [];
    server.use(
      http.put(apiPath('notification-channels/4'), async ({ request }) => {
        sent.push(await request.json());
        return json(opsHook);
      }),
    );
    renderApp('/alerts/channels');
    await user.click(await screen.findByRole('button', { name: 'Ops' }));
    const dialog = await screen.findByRole('dialog', { name: 'Change channel' });
    expect(within(dialog).getByText('Leave empty to keep the one stored.')).toBeVisible();
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(sent).toEqual([{ type: 'webhook', name: 'Ops', enabled: true, config: {} }]);
    });
  });
});

describe('alert text', () => {
  it.each([
    [
      {
        type: 'metric',
        config: { metric: 'temperature', direction: 'below', threshold: 5, minutes: 1 },
      },
      'Temperature below 5 °C for 1 minute',
    ],
    [{ type: 'offline', config: { minutes: 10 } }, 'Offline for more than 10 minutes'],
    [{ type: 'storage', config: {} }, 'Pool or disk problems, or more than 80% full'],
    [{ type: 'script_failed', config: {} }, 'A scheduled script fails'],
    [{ type: 'weather', config: {} }, 'weather'],
  ])('describes %j as "%s"', (rule, want) => {
    expect(describeCondition(rule)).toBe(want);
  });

  it('names a rule’s machines', () => {
    const names = new Map([['a', 'nas']]);
    expect(describeTarget({ kind: 'all', value: '' }, names)).toBe('All machines');
    expect(describeTarget({ kind: 'tag', value: 'homelab' }, names)).toBe(
      'Machines tagged homelab',
    );
    expect(describeTarget({ kind: 'agent', value: 'a' }, names)).toBe('nas');
    expect(describeTarget({ kind: 'agent', value: 'gone' }, names)).toBe('One machine');
  });
});
