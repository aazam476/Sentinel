// SPDX-License-Identifier: Apache-2.0
import { expect, test } from '@playwright/test';

import {
  expectAccessible,
  expectNoHorizontalScroll,
  signInAsNewAdmin,
  watchForCSPViolations,
} from './helpers';
import { Agent, agentFlags, OwnServer } from './processes';

/**
 * M6 "Done when", for scripts: a script written in the UI, scheduled every
 * minute on a tag of machines, runs on the real agent by itself, and its
 * output is viewable. The scheduler checks once a minute, so this waits up
 * to three minutes for the first scheduled run.
 */
const ports: Record<string, number> = {
  desktop: 7530,
  phone: 7531,
  'desktop-reduced-motion': 7532,
  'phone-reduced-motion': 7533,
};

test('M6: a scheduled script runs on a tag of machines and its output is viewable', async ({
  page,
}, testInfo) => {
  test.setTimeout(300_000);
  const server = new OwnServer(
    ports[testInfo.project.name] ?? 7534,
    `scripts-${testInfo.project.name}`,
  );
  const agent = new Agent(`scripts-${testInfo.project.name}`);
  await server.start();
  try {
    const cspViolations = watchForCSPViolations(page);
    await signInAsNewAdmin(page, server);
    await page.goto(`${server.origin}/machines/add`);
    await page.getByRole('button', { name: 'Create install command' }).click();
    agent.enroll(agentFlags(await page.locator('code').first().innerText()));
    agent.run();
    await page.goto(`${server.origin}/`);
    await page.getByRole('link', { name: /Online/ }).click({ timeout: 15_000 });
    await page.waitForURL(/\/machines\/[0-9A-Z]{26}$/);
    const machineId = new URL(page.url()).pathname.split('/').pop() ?? '';
    const tagged = await page.request.patch(`${server.origin}/api/agents/${machineId}`, {
      data: { tags: ['scripted'] },
      headers: { Origin: server.origin },
    });
    expect(tagged.status()).toBe(200);

    // A script, written in the UI.
    await page.goto(`${server.origin}/scripts`);
    await page.getByRole('link', { name: 'New script' }).click();
    await page.getByRole('textbox', { name: 'Name' }).fill('Greeting');
    await page.getByRole('combobox', { name: 'Language' }).selectOption('sh');
    await page.getByRole('textbox', { name: 'Script', exact: true }).click();
    await page.keyboard.press('ControlOrMeta+A');
    await page.keyboard.insertText('echo "scheduled hello from $(uname -s)"');
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByText('Saved Greeting')).toBeVisible();

    // Scheduled every minute on the tag.
    await page.getByRole('link', { name: 'Schedules' }).click();
    await page.getByRole('button', { name: 'New schedule' }).click();
    const dialog = page.getByRole('dialog', { name: 'New schedule' });
    await dialog.getByRole('textbox', { name: 'When (cron)' }).fill('* * * * *');
    await expect(dialog.getByText('Every minute')).toBeVisible();
    await dialog.getByRole('combobox', { name: 'Tag' }).selectOption('scripted');
    await dialog.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByRole('listitem', { name: 'Schedule of Greeting' })).toBeVisible();

    // The scheduler runs it by itself; its output is viewable.
    await page.goto(`${server.origin}/scripts/runs`);
    const scheduled = page
      .getByRole('listitem', { name: /^Greeting,/ })
      .filter({ hasText: 'on a schedule' })
      .first();
    await expect(async () => {
      await page.reload();
      await expect(scheduled).toBeVisible({ timeout: 2_000 });
    }).toPass({ timeout: 200_000, intervals: [5_000] });
    await scheduled.getByRole('link', { name: 'Greeting' }).click();
    await expect(page.getByText('Succeeded').first()).toBeVisible({ timeout: 30_000 });
    await expect(
      page.getByRole('region', { name: 'Output' }).or(page.getByLabel('Output')),
    ).toContainText('scheduled hello from');
    await expectNoHorizontalScroll(page);
    await expectAccessible(page);

    const audit = await page.request.get(`${server.origin}/api/audit?limit=100`);
    const actions = ((await audit.json()) as { entries: { action: string }[] }).entries.map(
      (entry) => entry.action,
    );
    for (const action of ['script.save', 'schedule.save', 'script.run']) {
      expect(actions).toContain(action);
    }
    expect(cspViolations).toEqual([]);
  } finally {
    agent.stop();
    await server.stop();
  }
});
