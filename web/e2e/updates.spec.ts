// SPDX-License-Identifier: Apache-2.0
import { expect, type Page, test } from '@playwright/test';
import { join } from 'node:path';

import { openOnlineMachine, signInAsNewAdmin, watchForCSPViolations } from './helpers';
import { Agent, agentFlags, OwnServer, signedAgentBuild } from './processes';

/**
 * M7 "Done when": a real agent updates itself, and refuses, rolling back
 * automatically, a deliberately broken build (SEC-31, SEC-32). Both builds
 * are signed as releases with the development key, uploaded in the UI,
 * and pushed from the machine's page.
 */
const ports: Record<string, number> = {
  desktop: 7560,
  phone: 7561,
  'desktop-reduced-motion': 7562,
  'phone-reduced-motion': 7563,
};

async function uploadBuild(page: Page, build: { build: string; signature: string }) {
  const upload = page.getByRole('region', { name: 'Upload a build' });
  await upload.getByLabel('Agent binary').setInputFiles(build.build);
  await upload.getByLabel('Signature (.sig)').setInputFiles(build.signature);
  await upload.getByRole('button', { name: 'Upload' }).click();
  await expect(page.getByText(/^Added v9\.9\.\d for /)).toBeVisible();
}

test('M7: an agent updates itself, and rolls back from a broken build', async ({
  page,
}, testInfo) => {
  test.setTimeout(300_000);
  const name = `updates-${testInfo.project.name}`;
  const server = new OwnServer(ports[testInfo.project.name] ?? 7564, name);
  const agent = new Agent(name);
  const builds = join(agent.root, 'builds');
  const broken = signedAgentBuild(join(builds, 'broken'), 'v9.9.8', true);
  const good = signedAgentBuild(join(builds, 'good'), 'v9.9.9');
  await server.start();
  try {
    const cspViolations = watchForCSPViolations(page);
    await signInAsNewAdmin(page, server);
    await page.goto(`${server.origin}/machines/add`);
    await page.getByRole('button', { name: 'Create install command' }).click();
    agent.enroll(agentFlags(await page.locator('code').first().innerText()));
    agent.run();
    await page.goto(`${server.origin}/`);
    await openOnlineMachine(page);
    const machine = page.url();

    // The broken build is signed, so it is accepted and offered...
    await page.goto(`${server.origin}/updates`);
    await uploadBuild(page, broken);
    await page.goto(machine);
    const panel = page.getByRole('region', { name: 'Agent' });
    await panel.getByRole('button', { name: 'Update to v9.9.8' }).click();
    // ...but it fails its trial, so the agent never installs it and says so.
    await expect(
      panel.getByText(/Couldn’t update to v9\.9\.8.*failed before connecting/),
    ).toBeVisible({
      timeout: 120_000,
    });
    await expect(panel.getByText(/^Running /)).toBeVisible();
    await expect(panel).not.toContainText('Running v9.9.8');
    expect(agent.restarts).toBe(0);

    // A working build installs, and the agent comes back as it.
    await page.goto(`${server.origin}/updates`);
    await uploadBuild(page, good);
    await page.goto(machine);
    await panel.getByRole('button', { name: 'Update to v9.9.9' }).click();
    await expect(panel.getByText('Running v9.9.9.')).toBeVisible({ timeout: 120_000 });
    await expect(panel.getByText(/^Updated to v9\.9\.9/)).toBeVisible({ timeout: 30_000 });
    expect(agent.restarts).toBe(1);

    const audit = await page.request.get(`${server.origin}/api/audit?limit=100`);
    const entries = ((await audit.json()) as { entries: { action: string; outcome: string }[] })
      .entries;
    const results = entries.filter((entry) => entry.action === 'agent.update.result');
    expect(results.map((entry) => entry.outcome).sort()).toEqual(['failure', 'success']);
    expect(entries.filter((entry) => entry.action === 'agent.build.add')).toHaveLength(2);
    expect(cspViolations).toEqual([]);
  } finally {
    agent.stop();
    await server.stop();
  }
});
