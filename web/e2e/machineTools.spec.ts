// SPDX-License-Identifier: Apache-2.0
import { expect, test } from '@playwright/test';

import {
  expectAccessible,
  expectNoHorizontalScroll,
  signInAsNewAdmin,
  watchForCSPViolations,
  openOnlineMachine,
} from './helpers';
import { Agent, agentFlags, OwnServer } from './processes';

/**
 * M3 "Done when", as far as a test machine allows: an operator opens a
 * shell on the machine and runs a command, sees its processes, services
 * and logs, and the terminal session is in the audit log. Rebooting is
 * covered by the component and Go tests; it would reboot the test runner.
 * Runs the real sentinel-agent against its own server.
 */
const ports: Record<string, number> = {
  desktop: 7494,
  phone: 7495,
  'desktop-reduced-motion': 7496,
  'phone-reduced-motion': 7497,
};

test('M3: a shell, processes, services and logs on a real machine', async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const server = new OwnServer(
    ports[testInfo.project.name] ?? 7498,
    `tools-${testInfo.project.name}`,
  );
  const agent = new Agent(`tools-${testInfo.project.name}`);
  await server.start();
  try {
    const cspViolations = watchForCSPViolations(page);
    await signInAsNewAdmin(page, server);
    await page.goto(`${server.origin}/machines/add`);
    await page.getByRole('button', { name: 'Create install command' }).click();
    const command = await page.locator('code').first().innerText();
    agent.enroll(agentFlags(command));
    agent.run();
    await page.goto(`${server.origin}/`);
    await openOnlineMachine(page);

    // The terminal runs a real shell, under the Content-Security-Policy.
    await page.getByRole('link', { name: 'Terminal' }).click();
    await expect(page.getByRole('status').getByText('Connected')).toBeVisible({ timeout: 15_000 });
    // Typing waits for the shell's prompt, as a person would: a login shell
    // on a busy machine can take a moment to start reading.
    const screen = page.locator('.xterm-rows');
    await expect(screen).toHaveText(/\S/, { timeout: 15_000 });
    await page.locator('.xterm').click();
    await page.keyboard.type('echo sentinel-$((40 + 2))\n');
    await expect(screen).toContainText('sentinel-42', { timeout: 15_000 });
    await expectNoHorizontalScroll(page);
    await page.getByRole('button', { name: 'End session' }).click();
    await expect(page.getByText('You closed the session.')).toBeVisible();

    // The agent itself is among the machine's processes (other projects'
    // agents run on the same machine, so there may be several).
    await page.getByRole('link', { name: 'Processes' }).click();
    await page.getByRole('searchbox', { name: 'Search processes' }).fill('sentinel-agent');
    await expect(
      page.getByRole('article', { name: /^sentinel-agent \(\d+\)$/ }).first(),
    ).toBeVisible({
      timeout: 15_000,
    });
    await expectNoHorizontalScroll(page);
    await expectAccessible(page);

    // Services and logs come from the machine's own tools. A test container
    // may lack a running systemd or journal, so either a list or the
    // machine's explanation counts; neither may break the page.
    await page.getByRole('link', { name: 'Services' }).click();
    const services = page.getByRole('region', { name: 'Services' });
    const servicesError = page.getByRole('alert');
    await expect(services.or(servicesError).first()).toBeVisible({ timeout: 15_000 });
    await expectNoHorizontalScroll(page);
    await page.getByRole('link', { name: 'Logs' }).click();
    const logs = page.getByRole('region', { name: 'Logs' });
    await expect(logs).toBeVisible();
    await expect(logs.getByText('Reading the log…')).toHaveCount(0, { timeout: 15_000 });
    await expectNoHorizontalScroll(page);
    await expectAccessible(page);

    // The terminal session is in the audit log, start and end.
    await page.goto(`${server.origin}/audit`);
    await expect(page.getByRole('article', { name: /terminal\.close/ })).toBeVisible();
    await expect(page.getByRole('article', { name: /terminal\.open/ })).toBeVisible();
    expect(cspViolations).toEqual([]);
  } finally {
    agent.stop();
    await server.stop();
  }
});
