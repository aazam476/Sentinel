// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from '@playwright/test';

import {
  worstAgents,
  worstAuditEntries,
  worstContainers,
  worstLogs,
  longDisplayName,
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
  worstMachineAlerts,
  worstRules,
  worstUsers,
  worstAbout,
  worstSettings,
  worstPackages,
  worstBuilds,
  worstFolder,
  worstFolderLink,
} from '../src/test/fixtures/worst-case/fixtures';
import { expectAccessible, expectNoHorizontalScroll } from './helpers';

/**
 * Renders every screen that shows server data with the worst-case fixtures
 * (longest names, unbreakable strings, right-to-left text, 1,200 audit
 * entries) by answering the API from the fixtures, and checks in a real
 * browser that nothing sticks out of the viewport and nothing is cut short
 * without a tooltip.
 */
async function serveWorstCase(page: Page) {
  const responses: Record<string, unknown> = {
    'setup/status': { setupRequired: false },
    'auth/session': worstSession,
    'auth/passkeys': { passkeys: worstPasskeys },
    'auth/sessions': { sessions: worstSessions },
    audit: { entries: worstAuditEntries(), nextBefore: null },
    agents: { agents: worstAgents() },
    [`agents/${worstMachine.id}`]: worstMachine,
    [`agents/${worstMachine.id}/services`]: worstServices(),
    [`agents/${worstMachine.id}/processes`]: worstProcesses(),
    [`agents/${worstMachine.id}/logs`]: worstLogs(),
    [`agents/${worstMachine.id}/docker/containers`]: worstContainers(),
    [`agents/${worstMachine.id}/docker/stacks`]: worstStacks,
    [`agents/${worstMachine.id}/storage`]: worstStorage(),
    [`agents/${worstMachine.id}/files`]: worstFolder(),
    scripts: worstScripts(),
    'script-batches/3': worstBatch(),
    'script-runs/101': worstRunOutput(),
    schedules: worstSchedules,
    [`agents/${worstMachine.id}/alerts`]: worstMachineAlerts,
    alerts: worstFleetAlerts(),
    'alert-rules': worstRules,
    'notification-channels': worstChannels,
    users: worstUsers(),
    'agent-builds': worstBuilds(),
    about: worstAbout(),
    settings: worstSettings(),
    [`agents/${worstMachine.id}/packages`]: worstPackages(),
    [`agents/${worstMachine.id}/metrics`]: { metric: 'cpu.percent', resolution: 'raw', points: [] },
    'enrollment-tokens': { tokens: worstTokens, serverFingerprint: 'SHA256:x', tlsFingerprint: '' },
  };
  await page.route('**/api/**', (route) => {
    const path = new URL(route.request().url()).pathname.replace(/^\/api\//, '');
    const body = responses[path];
    return body === undefined
      ? route.fulfill({
          status: 404,
          json: { error: { code: 'not_found', message: 'not mocked' } },
        })
      : route.fulfill({ json: body });
  });
}

/** expectNothingCutWithoutTooltip finds ellipsis-truncated text without a title. */
async function expectNothingCutWithoutTooltip(page: Page) {
  const cut = await page.evaluate(() =>
    [...document.querySelectorAll<HTMLElement>('body *')]
      .filter(
        (element) =>
          getComputedStyle(element).textOverflow === 'ellipsis' &&
          element.scrollWidth > element.clientWidth,
      )
      .filter((element) => element.title === '')
      .map((element) => element.textContent),
  );
  expect(cut).toEqual([]);
}

/** Each screen and what shows once it has loaded: a heading or a region. */
const screens: {
  path: string;
  ready: string | RegExp;
  role?: 'heading' | 'region';
  /** allowAll serves the machine with no capability disabled. */
  allowAll?: boolean;
}[] = [
  { path: '/', ready: /^Machines$/ },
  { path: '/account', ready: 'Passkeys' },
  { path: '/audit', ready: 'Audit log' },
  { path: `/machines/${worstMachine.id}`, ready: 'System' },
  { path: `/machines/${worstMachine.id}/services`, ready: 'Services', role: 'region' },
  { path: `/machines/${worstMachine.id}/processes`, ready: 'Processes', role: 'region' },
  { path: `/machines/${worstMachine.id}/logs`, ready: 'Logs', role: 'region' },
  { path: `/machines/${worstMachine.id}/docker`, ready: 'Containers', role: 'region' },
  { path: `/machines/${worstMachine.id}/docker/stacks`, ready: 'Stacks', role: 'region' },
  { path: `/machines/${worstMachine.id}/storage`, ready: 'Pools', role: 'region', allowAll: true },
  { path: worstFolderLink, ready: 'Files', role: 'region', allowAll: true },
  {
    path: `/machines/${worstMachine.id}/updates`,
    ready: 'Updates',
    role: 'region',
    allowAll: true,
  },
  // No desktop to share: the longest reason the server keeps.
  { path: `/machines/${worstMachine.id}/desktop`, ready: longDisplayName, allowAll: true },
  { path: '/machines/add', ready: 'Add a machine' },
  { path: '/scripts', ready: 'Script library', role: 'region' },
  { path: '/scripts/runs/3', ready: /^Run of /, role: 'region' },
  { path: '/scripts/schedules', ready: 'Schedules', role: 'region' },
  { path: '/alerts', ready: 'Alerts', role: 'region' },
  { path: '/alerts/rules', ready: 'Alert rules', role: 'region' },
  { path: '/alerts/channels', ready: 'Notification channels', role: 'region' },
  { path: '/users', ready: 'Users', role: 'region' },
  { path: '/updates', ready: 'Agent builds', role: 'region' },
  { path: '/about', ready: 'Agent versions', role: 'region' },
  { path: '/settings', ready: 'Server (server.toml)', role: 'region' },
];

for (const screen of screens) {
  test(`worst-case data fits on ${screen.path}`, async ({ page }) => {
    await serveWorstCase(page);
    if (screen.allowAll === true) {
      await page.route(`**/api/agents/${worstMachine.id}`, (route) =>
        route.fulfill({ json: { ...worstMachine, capabilities: [] } }),
      );
    }
    await page.goto(screen.path);
    await expect(
      page.getByRole(screen.role ?? 'heading', { name: screen.ready }).first(),
    ).toBeVisible();
    await expect(page.getByText('Loading', { exact: false })).toHaveCount(0);

    await expectNoHorizontalScroll(page);
    await expectNothingCutWithoutTooltip(page);
    await expectAccessible(page);
  });
}

test('worst-case username fits on the second-factor step', async ({ page }) => {
  await serveWorstCase(page);
  await page.route('**/api/auth/session', (route) =>
    route.fulfill({ json: { ...worstSession, state: 'second_factor', stepUpValidUntil: null } }),
  );
  await page.goto('/login/verify');
  await expect(page.getByRole('heading', { name: 'Confirm it’s you' })).toBeVisible();

  await expectNoHorizontalScroll(page);
  await expectAccessible(page);
});
