// SPDX-License-Identifier: Apache-2.0
import { expect, test } from '@playwright/test';
import { spawnSync } from 'node:child_process';
import { join } from 'node:path';

import { expectNoHorizontalScroll, signInAsNewAdmin, watchForCSPViolations } from './helpers';
import { Agent, agentFlags, OwnServer } from './processes';

/**
 * M4 "Done when": create a stack from a pasted compose file, deploy it,
 * view its logs, open a shell in its container, pull updates and
 * redeploy, entirely from the UI, against the machine's real Docker. The
 * compose editor and terminal run under the real Content-Security-Policy.
 */
const ports: Record<string, number> = {
  desktop: 7500,
  phone: 7501,
  'desktop-reduced-motion': 7502,
  'phone-reduced-motion': 7503,
};

const hasDocker = spawnSync('docker', ['info'], { stdio: 'ignore' }).status === 0;

test('M4: a stack from a pasted compose file, its logs, a shell, and an update', async ({
  page,
}, testInfo) => {
  test.skip(!hasDocker, 'Docker is not available on this machine');
  test.setTimeout(240_000);
  const project = testInfo.project.name.replaceAll(/[^a-z]/g, '');
  const stack = `e2e${project}`;
  const server = new OwnServer(
    ports[testInfo.project.name] ?? 7509,
    `docker-${testInfo.project.name}`,
  );
  const agent = new Agent(`docker-${testInfo.project.name}`);
  agent.writePolicy(`[docker]\nstacks_dir = "${join(agent.root, 'stacks')}"\n`);
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

    // A new stack, pasted into the compose editor, deployed.
    await page.getByRole('link', { name: 'Docker' }).click();
    await page.getByRole('link', { name: 'Stacks' }).click();
    await page.getByRole('link', { name: 'New stack' }).click();
    await page.getByRole('textbox', { name: 'Stack name' }).fill(stack);
    await page.getByRole('textbox', { name: 'compose.yaml' }).click();
    await page.keyboard.insertText(
      'services:\n  app:\n    image: alpine:3.20\n    pull_policy: missing\n    command: ["sh", "-c", "echo hello from sentinel; sleep 3600"]\n    environment:\n      GREETING: ${GREETING}\n',
    );
    await page.getByRole('textbox', { name: '.env' }).click();
    await page.keyboard.insertText('GREETING=hi-from-env\n');
    await page.getByRole('button', { name: 'Validate' }).click();
    await expect(page.getByText('The compose file is valid')).toBeVisible({ timeout: 30_000 });
    // On a phone the toast sits over the buttons; wait for it, as a person
    // would. Sonner holds a toast while the pointer is over it, so the
    // pointer moves away first (a phone has no hover).
    await page.mouse.move(0, 0);
    await expect(page.getByText('The compose file is valid')).toBeHidden({ timeout: 15_000 });
    await page.getByRole('button', { name: 'Save and deploy' }).click();
    await expect(page.getByText(`Deployed ${stack}`)).toBeVisible({ timeout: 120_000 });
    await expectNoHorizontalScroll(page);

    // Its container's log.
    await page.getByRole('link', { name: 'Containers' }).click();

    const container = `${stack}-app-1`;
    await page.getByRole('button', { name: `Actions for ${container}` }).click();
    await page.getByRole('menuitem', { name: 'Logs' }).click();
    await expect(page.getByText('hello from sentinel')).toBeVisible({ timeout: 30_000 });

    // A shell in it, with the stack's .env applied.
    await page.getByRole('link', { name: '← Containers' }).click();
    await page.getByRole('button', { name: `Actions for ${container}` }).click();
    await page.getByRole('menuitem', { name: 'Shell' }).click();
    await expect(page.getByRole('status').getByText('Connected')).toBeVisible({ timeout: 15_000 });
    const screen = page.locator('.xterm-rows');
    await expect(screen).toHaveText(/\S/, { timeout: 15_000 });
    await page.locator('.xterm').click();
    await page.keyboard.type('echo "$GREETING"\n');
    await expect(screen).toContainText('hi-from-env', { timeout: 15_000 });
    await page.getByRole('button', { name: 'End session' }).click();

    // Pull updates and redeploy, then stop it. The image says
    // pull_policy: missing, so the pull is skipped rather than depending on
    // a public registry (and its rate limits) in the test; the agent's own
    // tests cover pulling.
    await page.getByRole('link', { name: '← Containers' }).click();
    await page.getByRole('link', { name: 'Stacks' }).click();
    await page.getByRole('link', { name: stack, exact: true }).click();
    await page.getByRole('button', { name: 'Pull and redeploy' }).click();
    await expect(page.getByText(`Redeployed ${stack}`)).toBeVisible({ timeout: 180_000 });
    await page.getByRole('button', { name: 'Stop' }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: 'Stop' }).click();
    await expect(page.getByText(`Stopped ${stack}`)).toBeVisible({ timeout: 60_000 });

    // Every change is in the audit log.
    const audit = await page.request.get(`${server.origin}/api/audit?limit=100`);
    const actions = ((await audit.json()) as { entries: { action: string }[] }).entries.map(
      (entry) => entry.action,
    );
    for (const action of [
      'docker.stack.save',
      'docker.stack.up',
      'docker.exec.open',
      'docker.stack.pull',
      'docker.stack.down',
    ]) {
      expect(actions).toContain(action);
    }
    expect(cspViolations).toEqual([]);
  } finally {
    spawnSync('docker', ['compose', '--project-name', stack, 'down'], { stdio: 'ignore' });
    agent.stop();
    await server.stop();
  }
});
