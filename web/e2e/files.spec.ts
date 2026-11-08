// SPDX-License-Identifier: Apache-2.0
import { expect, test } from '@playwright/test';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import {
  expectAccessible,
  expectNoHorizontalScroll,
  signInAsNewAdmin,
  watchForCSPViolations,
} from './helpers';
import { Agent, agentFlags, OwnServer } from './processes';

/**
 * M6's file manager on a real machine: upload a file bigger than one
 * piece, download it again, edit a text file, create, rename and delete a
 * folder, all within the one folder the local policy allows (SEC-25), and
 * every change in the audit log.
 */
const ports: Record<string, number> = {
  desktop: 7520,
  phone: 7521,
  'desktop-reduced-motion': 7522,
  'phone-reduced-motion': 7523,
};

test('M6: files on a real machine, within the allowed folder', async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const server = new OwnServer(
    ports[testInfo.project.name] ?? 7524,
    `files-${testInfo.project.name}`,
  );
  const agent = new Agent(`files-${testInfo.project.name}`);
  const share = join(agent.root, 'share');
  mkdirSync(share, { recursive: true });
  writeFileSync(join(share, 'notes.txt'), 'first line\n');
  agent.writePolicy(`[files]\nallowed_paths = [${JSON.stringify(share)}]\n`);
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

    // The start is the allowed folder alone.
    await page.getByRole('link', { name: 'Files' }).click();
    await page.getByRole('button', { name: share, exact: true }).click();
    const files = page.getByRole('region', { name: 'Files' });
    await expect(files.getByRole('listitem', { name: 'notes.txt' })).toBeVisible();

    // An upload of two pieces (the browser sends 8 MB at a time).
    const upload = Buffer.alloc(9 * 1024 * 1024 + 123, 'sentinel');
    await page
      .getByLabel('Files to upload')
      .setInputFiles({ name: 'big.bin', mimeType: 'application/octet-stream', buffer: upload });
    await expect(page.getByRole('list', { name: 'Uploads' }).getByText(/^Uploaded/)).toBeVisible({
      timeout: 30_000,
    });
    expect(readFileSync(join(share, 'big.bin')).equals(upload)).toBe(true);

    // Downloading it gives back the same bytes.
    await files.getByRole('button', { name: 'Actions for big.bin' }).click();
    const href = await page.getByRole('menuitem', { name: 'Download' }).getAttribute('href');
    await page.keyboard.press('Escape');
    const download = await page.request.get(href ?? '');
    expect(download.status()).toBe(200);
    expect((await download.body()).equals(upload)).toBe(true);

    // Edit the text file in the browser.
    await files.getByRole('button', { name: 'Actions for notes.txt' }).click();
    await page.getByRole('menuitem', { name: 'Edit' }).click();
    const editor = page.getByRole('textbox', { name: join(share, 'notes.txt') });
    await editor.click();
    await page.keyboard.press('ControlOrMeta+End');
    await page.keyboard.insertText('second line\n');
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByText('Saved notes.txt')).toBeVisible();
    expect(readFileSync(join(share, 'notes.txt'), 'utf8')).toBe('first line\nsecond line\n');
    await page.getByRole('button', { name: 'Close' }).click();

    // A folder made, renamed and deleted.
    await page.getByRole('button', { name: 'New folder' }).click();
    await page.getByRole('textbox', { name: 'Folder name' }).fill('drafts');
    await page.getByRole('button', { name: 'Create' }).click();
    await files.getByRole('button', { name: 'Actions for drafts' }).click();
    await page.getByRole('menuitem', { name: 'Rename' }).click();
    await page.getByRole('textbox', { name: 'New name' }).fill('archive');
    await page.getByRole('button', { name: 'Rename' }).click();
    await files.getByRole('button', { name: 'Actions for archive' }).click();
    await page.getByRole('menuitem', { name: 'Delete' }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click();
    await expect(files.getByRole('listitem', { name: 'archive' })).toHaveCount(0);
    await expectNoHorizontalScroll(page);
    await expectAccessible(page);

    // SEC-25: the folder above the allowed one is out of reach.
    await page.getByRole('textbox', { name: 'Go to a folder' }).fill(agent.root);
    await page.getByRole('button', { name: 'Go' }).click();
    await expect(page.getByText(/local policy doesn.t allow the file manager there/)).toBeVisible();

    const audit = await page.request.get(`${server.origin}/api/audit?limit=100`);
    const actions = ((await audit.json()) as { entries: { action: string }[] }).entries.map(
      (entry) => entry.action,
    );
    for (const action of [
      'file.upload',
      'file.download',
      'file.text.write',
      'file.mkdir',
      'file.rename',
      'file.delete',
    ]) {
      expect(actions).toContain(action);
    }
    expect(cspViolations).toEqual([]);
  } finally {
    agent.stop();
    await server.stop();
  }
});
