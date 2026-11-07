// SPDX-License-Identifier: Apache-2.0
import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

// M0 "Done when": the server serves a "Hello Sentinel" page.
test.describe('hello page', () => {
  test('the server serves the Hello Sentinel page', async ({ page }) => {
    await page.goto('/');

    await expect(page).toHaveTitle('Sentinel');
    await expect(page.getByRole('heading', { level: 1, name: 'Hello Sentinel' })).toBeVisible();
    await expect(page.getByText('The server is running.')).toBeVisible();
  });

  test('has no accessibility violations', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

    const results = await new AxeBuilder({ page }).analyze();

    expect(results.violations.map((violation) => violation.id)).toEqual([]);
  });

  test('fits the viewport without horizontal scrolling', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );

    expect(overflow).toBe(0);
  });
});
