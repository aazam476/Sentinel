// SPDX-License-Identifier: Apache-2.0
import { expect, test } from '@playwright/test';

import { expectAccessible, signInAsNewAdmin, watchForCSPViolations } from './helpers';
import { OwnServer } from './processes';

/**
 * M6, users: an admin adds an operator in the UI, who must set up
 * two-factor authentication on first sign-in; then the admin disables them.
 */
const ports: Record<string, number> = {
  desktop: 7550,
  phone: 7551,
  'desktop-reduced-motion': 7552,
  'phone-reduced-motion': 7553,
};

test('M6: an admin adds a user, who sets up 2FA first, and disables them', async ({
  page,
  browser,
}, testInfo) => {
  const server = new OwnServer(
    ports[testInfo.project.name] ?? 7554,
    `users-${testInfo.project.name}`,
  );
  await server.start();
  try {
    const cspViolations = watchForCSPViolations(page);
    await signInAsNewAdmin(page, server);
    await page.goto(`${server.origin}/`);
    await page
      .getByRole('navigation', { name: 'Main' })
      .getByRole('link', { name: 'Users' })
      .click();
    await expect(page.getByRole('listitem', { name: 'admin' })).toContainText('You');
    await page.getByRole('button', { name: 'Add user' }).click();
    const dialog = page.getByRole('dialog', { name: 'Add a user' });
    await dialog.getByRole('textbox', { name: 'Username' }).fill('olu');
    await dialog.getByLabel('First password').fill('a long first password');
    await dialog.getByRole('combobox', { name: 'Role' }).selectOption('operator');
    await dialog.getByRole('button', { name: 'Add user' }).click();
    const olu = page.getByRole('listitem', { name: 'olu' });
    await expect(olu.getByRole('combobox', { name: 'Role' })).toHaveValue('operator');
    await expectAccessible(page);

    // Their first sign-in goes to two-factor setup.
    const theirs = await browser.newContext({ ignoreHTTPSErrors: true });
    const them = await theirs.newPage();
    await them.goto(`${server.origin}/login`);
    await them.getByRole('textbox', { name: 'Username' }).fill('olu');
    await them.getByLabel('Password').fill('a long first password');
    await them.getByRole('button', { name: 'Sign in' }).click();
    await expect(
      them.getByRole('heading', { name: 'Set up two-factor authentication' }),
    ).toBeVisible();

    // Disabled, they can't sign in.
    await olu.getByRole('button', { name: 'Disable' }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: 'Disable' }).click();
    await expect(olu.getByText('Disabled')).toBeVisible();
    await them.goto(`${server.origin}/login`);
    await them.getByRole('textbox', { name: 'Username' }).fill('olu');
    await them.getByLabel('Password').fill('a long first password');
    await them.getByRole('button', { name: 'Sign in' }).click();
    await expect(them.getByRole('alert')).toBeVisible();
    await theirs.close();

    const audit = await page.request.get(`${server.origin}/api/audit?limit=100`);
    const actions = ((await audit.json()) as { entries: { action: string }[] }).entries.map(
      (entry) => entry.action,
    );
    expect(actions).toContain('user.create');
    expect(actions).toContain('user.disable');
    expect(cspViolations).toEqual([]);
  } finally {
    await server.stop();
  }
});
