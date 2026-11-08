// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Page } from '@playwright/test';

import {
  expectAccessible,
  expectNoHorizontalScroll,
  setupToken,
  totpCode,
  watchForCSPViolations,
} from './helpers';

// M1 "Done when": a fresh install can't be used without the setup token, and
// a user can't sign in without 2FA. The tests run in order against one fresh
// server per project, sharing what the setup created.
test.describe.configure({ mode: 'serial' });

const password = 'correct horse battery staple';
let totpSecret = '';
let recoveryCodes: string[] = [];

async function signInWithPassword(page: Page) {
  await page.goto('/login');
  await page.getByLabel('Username').fill('admin');
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Confirm it’s you' })).toBeVisible();
}

async function signOut(page: Page) {
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
}

test('SEC-01 a fresh install can only be set up with the setup token', async ({
  page,
  request,
}) => {
  const cspViolations = watchForCSPViolations(page);

  await page.goto('/account');
  await expect(page).toHaveURL(/\/setup$/);
  await expect(page.getByRole('heading', { name: 'Set up Sentinel' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  // The API refuses to sign anyone in before setup, whatever they send.
  const login = await request.post('api/auth/login', {
    data: { username: 'admin', password },
    headers: { Origin: new URL(page.url()).origin },
  });
  expect(login.status()).toBe(401);

  await page.getByLabel('Setup token').fill('NOT-THE-TOKEN');
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password').fill(password);
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByText('This setup token is incorrect.')).toBeVisible();
  expect(cspViolations).toEqual([]);
});

test('SEC-02 setup creates the admin with TOTP and recovery codes', async ({ page }, testInfo) => {
  await page.goto('/setup');
  await page.getByLabel('Setup token').fill(setupToken(testInfo));
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password').fill(password);
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByRole('img', { name: 'QR code for your authenticator app' })).toBeVisible();
  await page.getByText('Can’t scan it?').click();
  totpSecret = (await page.locator('code').innerText()).trim();
  await page.getByLabel('Authenticator code').fill(totpCode(totpSecret));
  await page.getByRole('button', { name: 'Confirm' }).click();

  const codes = page.getByRole('list', { name: 'Recovery codes' }).getByRole('listitem');
  await expect(codes).toHaveCount(10);
  recoveryCodes = await codes.allInnerTexts();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
  await page.getByLabel('I have saved these codes').check();
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('heading', { name: 'Machines', exact: true })).toBeVisible();
});

test('SEC-04 a password alone opens nothing; a second factor is required', async ({ page }) => {
  await signInWithPassword(page);

  // Every page of the app leads back to the second-factor step.
  await page.goto('/audit');
  await expect(page).toHaveURL(/\/login\/verify$/);
  await expectAccessible(page);

  await page.getByLabel('Authenticator code').fill('000000');
  await page.getByRole('button', { name: 'Verify' }).click();
  await expect(page.getByRole('alert')).toContainText('incorrect');

  await page.getByRole('button', { name: 'Use a recovery code' }).click();
  await page.getByLabel('Recovery code').fill(recoveryCodes[0] ?? '');
  // The failures so far (wrong token, a sign-in before setup, the wrong code)
  // put this client into a short backoff (SEC-05); retry until it passes.
  await expect(async () => {
    await page.getByRole('button', { name: 'Verify' }).click();
    await expect(page.getByRole('heading', { name: 'Machines', exact: true })).toBeVisible({
      timeout: 1000,
    });
  }).toPass({ timeout: 10_000 });
  await expect(page.getByText('9 recovery codes left')).toBeVisible();
});

test('SEC-04 a passkey can finish signing in', async ({ page }) => {
  const client = await page.context().newCDPSession(page);
  await client.send('WebAuthn.enable');
  await client.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2',
      transport: 'internal',
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
    },
  });

  await signInWithPassword(page);
  // Setup used this 30-second step's code, and each code works once. The
  // server accepts the next step's code too (for clock drift), as an
  // authenticator app a little ahead would show.
  await page.getByLabel('Authenticator code').fill(totpCode(totpSecret, Date.now() + 30_000));
  await page.getByRole('button', { name: 'Verify' }).click();
  await expect(page.getByRole('heading', { name: 'Machines', exact: true })).toBeVisible();

  await page.getByRole('link', { name: 'Account' }).click();
  await page.getByLabel('New passkey name').fill('Test authenticator');
  await page.getByRole('button', { name: 'Add passkey' }).click();
  await expect(page.getByText('Passkey added')).toBeVisible();
  await expect(page.getByText('Test authenticator')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await signOut(page);
  await signInWithPassword(page);
  await page.getByRole('button', { name: 'Use a passkey' }).click();
  await expect(page.getByRole('heading', { name: 'Machines', exact: true })).toBeVisible();
});

test('SEC-41 admins can read, verify and export the audit log', async ({ page }) => {
  await signInWithPassword(page);
  await page.getByRole('button', { name: 'Use a recovery code' }).click();
  await page.getByLabel('Recovery code').fill(recoveryCodes[1] ?? '');
  await page.getByRole('button', { name: 'Verify' }).click();

  await page.getByRole('link', { name: 'Audit log' }).click();
  // Newest first; the list only renders what is on screen, so scroll to the
  // end for setup itself.
  await expect(page.getByRole('article', { name: /auth\.passkey\.add/ })).toBeVisible();
  await expect(async () => {
    await page.evaluate(() => {
      window.scrollTo(0, document.documentElement.scrollHeight);
    });
    await expect(page.getByRole('article', { name: /setup\.complete/ })).toBeVisible({
      timeout: 500,
    });
  }).toPass({ timeout: 10_000 });
  await page.getByRole('button', { name: 'Verify integrity' }).click();
  await expect(page.getByRole('status')).toContainText('Intact');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  const download = page.waitForEvent('download');
  await page.getByRole('link', { name: 'Export CSV' }).click();
  expect((await download).suggestedFilename()).toMatch(/^sentinel-audit-\d{4}-\d{2}-\d{2}\.csv$/);
});
