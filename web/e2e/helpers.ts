// SPDX-License-Identifier: Apache-2.0
import AxeBuilder from '@axe-core/playwright';
import { expect, type Page, type TestInfo } from '@playwright/test';
import { createHmac } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import type { BeginSetupResponse } from '../src/api/types.gen';
import type { OwnServer } from './processes';

/** setupToken reads the token the project's server wrote on first start. */
export function setupToken(testInfo: TestInfo): string {
  return readFileSync(
    join('test-results', 'e2e-data', testInfo.project.name, 'setup-token'),
    'utf8',
  ).trim();
}

/**
 * totpCode computes the current code for a base32 secret, independently of
 * the server, like an authenticator app (RFC 6238: 30 s steps, 6 digits).
 */
export function totpCode(secret: string, at = Date.now()): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const character of secret.replace(/=+$/, '')) {
    bits += alphabet.indexOf(character).toString(2).padStart(5, '0');
  }
  const key = Buffer.from((bits.match(/.{8}/g) ?? []).map((byte) => parseInt(byte, 2)));
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 30_000)));
  const hash = createHmac('sha1', key).update(counter).digest();
  const offset = (hash[hash.length - 1] ?? 0) & 0x0f;
  return String((hash.readUInt32BE(offset) & 0x7fffffff) % 1_000_000).padStart(6, '0');
}

/**
 * expectAccessible runs axe on the page once its animations have finished,
 * so colors are measured as people see them, not halfway through a fade.
 */
export async function expectAccessible(page: Page) {
  await page.waitForFunction(() =>
    document.getAnimations().every((animation) => animation.playState !== 'running'),
  );
  const results = await new AxeBuilder({ page }).analyze();
  expect(
    results.violations.map(
      (violation) =>
        `${violation.id}: ${violation.nodes.map((node) => `${node.target.join(' ')} ${node.failureSummary ?? ''}`).join('; ')}`,
    ),
  ).toEqual([]);
}

/** expectNoHorizontalScroll checks nothing sticks out of the viewport. */
export async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBe(0);
}

/**
 * watchForCSPViolations collects every Content-Security-Policy violation,
 * so tests can prove the strict policy doesn't break the page.
 */
export function watchForCSPViolations(page: Page): string[] {
  const violations: string[] = [];
  page.on('console', (message) => {
    if (message.text().includes('Content Security Policy')) {
      violations.push(message.text());
    }
  });
  return violations;
}

/** signInAsNewAdmin sets the server up through its API, as the setup page would. */
export async function signInAsNewAdmin(page: Page, server: OwnServer) {
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
