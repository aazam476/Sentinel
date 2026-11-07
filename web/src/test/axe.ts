// SPDX-License-Identifier: Apache-2.0
import axe from 'axe-core';

/**
 * findAccessibilityViolations runs axe against a rendered container and
 * returns the IDs of every rule that failed, so a test can assert the list is
 * empty and the failure message names the broken rules.
 *
 * Color contrast is skipped because jsdom doesn't compute styles; the
 * end-to-end tests check it in a real browser.
 */
export async function findAccessibilityViolations(container: Element): Promise<string[]> {
  const results = await axe.run(container, {
    rules: { 'color-contrast': { enabled: false } },
  });
  return results.violations.map((violation) => violation.id);
}
