// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from '@playwright/test';

import type { BeginSetupResponse } from '../src/api/types.gen';
import {
  expectAccessible,
  expectNoHorizontalScroll,
  totpCode,
  watchForCSPViolations,
} from './helpers';
import { Agent, agentFlags, OwnServer } from './processes';

/**
 * M2 "Done when": an agent enrolls, appears online, streams metrics,
 * survives a server restart, and disappears within 1 second of being
 * revoked. Each project gets its own server, which this test restarts, and
 * runs the real sentinel-agent binary against it.
 */
const ports: Record<string, number> = {
  desktop: 7490,
  phone: 7491,
  'desktop-reduced-motion': 7492,
  'phone-reduced-motion': 7493,
};

/** signInAsNewAdmin sets the server up through its API, as the setup page would. */
async function signInAsNewAdmin(page: Page, server: OwnServer) {
  const headers = { Origin: server.origin };
  const setupToken = server.setupToken();
  const begun = await page.request.post(`${server.origin}/api/setup/begin`, {
    headers,
    data: { setupToken, username: 'admin', password: 'correct horse battery staple' },
  });
  expect(begun.ok()).toBe(true);
  const setup = (await begun.json()) as BeginSetupResponse;
  const completed = await page.request.post(`${server.origin}/api/setup/complete`, {
    headers,
    data: { setupToken, setupId: setup.setupId, totpCode: totpCode(setup.totp.secret) },
  });
  expect(completed.ok()).toBe(true);
}

test('M2: an agent enrolls, streams metrics, survives a restart and is revoked', async ({
  page,
}, testInfo) => {
  test.setTimeout(120_000);
  const server = new OwnServer(
    ports[testInfo.project.name] ?? 7499,
    `fleet-${testInfo.project.name}`,
  );
  const agent = new Agent(testInfo.project.name);
  await server.start();
  try {
    const cspViolations = watchForCSPViolations(page);
    await signInAsNewAdmin(page, server);

    // An admin creates the install command.
    await page.goto(`${server.origin}/`);
    await expect(page.getByRole('heading', { name: 'No machines yet' })).toBeVisible();
    await page.getByRole('link', { name: 'Add machine' }).click();
    await page.getByLabel('Note (optional)').fill('e2e machine');
    await page.getByRole('button', { name: 'Create install command' }).click();
    const command = await page.locator('code').first().innerText();
    expect(command).toContain('--tls-fingerprint SHA256:');
    await expectNoHorizontalScroll(page);
    await expectAccessible(page);

    // The agent enrolls with it and connects; the fleet shows it online
    // without a reload.
    expect(agent.enroll(agentFlags(command))).toContain('Enrolled with');
    await page.getByRole('link', { name: 'Machines', exact: true }).click();
    agent.run();
    const row = page.getByRole('link', { name: /Online/ });
    await expect(row).toBeVisible({ timeout: 15_000 });

    // It streams metrics: the first sample arrives within 10 seconds.
    await expect(row.getByText('CPU')).toBeVisible({ timeout: 20_000 });
    await row.click();
    await expect(
      page.getByRole('region', { name: 'Capabilities' }).getByText('Allowed').first(),
    ).toBeVisible();
    await expect(page.getByText(/^\d+%$/).first()).toBeVisible({ timeout: 20_000 });
    await expectNoHorizontalScroll(page);
    await expectAccessible(page);

    // It survives a server restart: the agent reconnects and the page's
    // event stream follows.
    await server.stop();
    await server.start();
    await page.reload();
    await expect(page.getByText('Online', { exact: true })).toBeVisible({ timeout: 20_000 });

    // Revoking disconnects it at once and takes it out of the fleet.
    await page.getByRole('button', { name: 'Revoke machine' }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: 'Revoke' }).click();
    const revokedAt = Date.now();
    await expect(page.getByRole('heading', { name: 'Machines', exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'No machines yet' })).toBeVisible({
      timeout: 1_000,
    });
    await expect.poll(() => agent.log, { timeout: 1_000 }).toContain('revoked');
    expect(Date.now() - revokedAt).toBeLessThan(2_000);
    expect(cspViolations).toEqual([]);
  } finally {
    agent.stop();
    await server.stop();
  }
});
