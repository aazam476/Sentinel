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
 * M5 on a real machine: the Storage tab reads the test machine's disks and
 * filesystems through the real agent, under the real Content-Security-
 * Policy. Pools need ZFS and root, so the nightly ZFS job checks them
 * against real pools, and the component tests check how they are shown.
 */
const ports: Record<string, number> = {
  desktop: 7510,
  phone: 7511,
  'desktop-reduced-motion': 7512,
  'phone-reduced-motion': 7513,
};

test('M5: the storage of a real machine', async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const server = new OwnServer(
    ports[testInfo.project.name] ?? 7514,
    `storage-${testInfo.project.name}`,
  );
  const agent = new Agent(`storage-${testInfo.project.name}`);
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

    await page.getByRole('link', { name: 'Storage' }).click();
    await expect(page.getByRole('region', { name: 'Physical disks' })).toBeVisible({
      timeout: 30_000,
    });
    const filesystems = page.getByRole('region', { name: 'Other filesystems' });
    await expect(filesystems).toBeVisible();
    // Every machine has a root filesystem; containers mount it as overlay,
    // a system mount, so the toggle may be needed to see it.
    const toggle = filesystems.getByRole('checkbox');
    if ((await toggle.count()) > 0) {
      await toggle.check();
    }
    await expect(
      filesystems.getByRole('listitem', { name: 'Filesystem /', exact: true }),
    ).toBeVisible();
    await expectNoHorizontalScroll(page);
    await expectAccessible(page);
    expect(cspViolations).toEqual([]);
  } finally {
    agent.stop();
    await server.stop();
  }
});
