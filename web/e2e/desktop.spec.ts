// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Locator, type Page } from '@playwright/test';

import {
  expectAccessible,
  expectNoHorizontalScroll,
  openOnlineMachine,
  signInAsNewAdmin,
  watchForCSPViolations,
} from './helpers';
import { spawnSync } from 'node:child_process';
import { join } from 'node:path';

import { Agent, agentFlags, OwnServer, XDisplay } from './processes';

/**
 * M8's remote desktop on a real agent sharing its test pattern: the screen
 * comes over WebRTC, then through the server, and a click on the canvas
 * comes back as a magenta mark where it landed, both ways.
 */
// Chromium hides its addresses behind mDNS names, which the agent resolves
// over multicast. With four sessions at once on one CI machine, that
// sometimes fails, and the session rightly falls back to the server after
// eight seconds; real addresses keep the direct path deterministic here.
test.use({ launchOptions: { args: ['--disable-features=WebRtcHideLocalIpsWithMdns'] } });

const ports: Record<string, number> = {
  desktop: 7570,
  phone: 7571,
  'desktop-reduced-motion': 7572,
  'phone-reduced-motion': 7573,
};

/** clickAndSeeMark clicks the canvas at a fraction of its size and waits for the mark. */
async function clickAndSeeMark(page: Page, canvas: Locator, fx: number, fy: number) {
  const box = await canvas.boundingBox();
  if (box === null) {
    throw new Error('the screen isn’t shown');
  }
  await page.mouse.click(box.x + box.width * fx, box.y + box.height * fy);
  // The mark is 24 pixels wide, centred on the click; the pointer's dot
  // covers its middle, so look a little to the side.
  const isMagenta = () =>
    canvas.evaluate(
      (element: HTMLCanvasElement, [x, y]) => {
        const pixel = element
          .getContext('2d')
          ?.getImageData(
            Math.round(element.width * x) + 8,
            Math.round(element.height * y),
            1,
            1,
          ).data;
        const [r, g, b] = [pixel?.[0] ?? 0, pixel?.[1] ?? 255, pixel?.[2] ?? 0];
        // Magenta, give or take what JPEG does to it.
        return r > 200 && g < 70 && b > 200;
      },
      [fx, fy],
    );
  await expect.poll(isMagenta, { timeout: 15_000 }).toBe(true);
}

test('M8: remote desktop on a real machine, direct and through the server', async ({
  page,
}, testInfo) => {
  test.setTimeout(120_000);
  const server = new OwnServer(
    ports[testInfo.project.name] ?? 7574,
    `desktop-${testInfo.project.name}`,
  );
  const agent = new Agent(`desktop-${testInfo.project.name}`);
  agent.writePolicy('[desktop]\ntest_pattern = true\n');
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

    await page.getByRole('link', { name: 'Desktop' }).click();
    const desktop = page.getByRole('region', { name: 'Remote desktop' });
    // The browser and the agent are on one machine, so WebRTC connects.
    await expect(desktop.getByRole('status')).toHaveText('Direct (WebRTC) · test pattern', {
      timeout: 20_000,
    });
    const canvas = desktop.getByLabel('Screen');
    await expect(canvas).toHaveJSProperty('width', 1280);
    await clickAndSeeMark(page, canvas, 0.25, 0.5);

    // Ctrl+V on the screen pastes this computer's clipboard there; the test
    // pattern answers with a copy of its own, which lands back here.
    await page
      .context()
      .grantPermissions(['clipboard-read', 'clipboard-write'], { origin: server.origin });
    await page.evaluate(() => navigator.clipboard.writeText('hello from the browser'));
    await page.keyboard.press('Control+V');
    await expect(
      desktop.getByText('Text copied on the machine is on your clipboard.'),
    ).toBeVisible();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
      'Copied on the test pattern: hello from the browser',
    );

    // The test pattern's halves stand in for two monitors: show the right
    // one alone, and a click there lands on it.
    await desktop
      .getByRole('combobox', { name: 'Monitor' })
      .selectOption({ label: 'Right half · 640×720' });
    await expect(canvas).toHaveJSProperty('width', 640);
    await clickAndSeeMark(page, canvas, 0.5, 0.25);
    await desktop
      .getByRole('combobox', { name: 'Monitor' })
      .selectOption({ label: 'All monitors' });
    await expect(canvas).toHaveJSProperty('width', 1280);
    await expectNoHorizontalScroll(page);
    await expectAccessible(page);

    // Through the server, as when no direct route exists.
    await desktop.getByRole('checkbox', { name: 'Relay through the server' }).check();
    await expect(desktop.getByRole('status')).toHaveText('Through the server · test pattern', {
      timeout: 20_000,
    });
    // A new session: wait for its screen's size before clicking.
    await expect(canvas).toHaveJSProperty('width', 1280);
    await clickAndSeeMark(page, canvas, 0.75, 0.5);

    await desktop.getByRole('button', { name: 'End session' }).click();
    await expect(desktop.getByRole('status')).toHaveText('You closed the session.');

    const audit = await page.request.get(`${server.origin}/api/audit?limit=100`);
    const actions = ((await audit.json()) as { entries: { action: string }[] }).entries.map(
      (entry) => entry.action,
    );
    expect(actions.filter((action) => action === 'desktop.open')).toHaveLength(2);
    expect(cspViolations).toEqual([]);
  } catch (error) {
    await testInfo.attach('agent log', { body: agent.log });
    console.log(agent.log);
    throw error;
  } finally {
    agent.stop();
    await server.stop();
  }
});

const x11Ports: Record<string, number> = {
  desktop: 7580,
  phone: 7581,
  'desktop-reduced-motion': 7582,
  'phone-reduced-motion': 7583,
};

/** pixel reads the canvas at a point of the machine's screen. */
function pixel(canvas: Locator, x: number, y: number) {
  return canvas.evaluate(
    (element: HTMLCanvasElement, [px, py]) => {
      const data = element.getContext('2d')?.getImageData(px, py, 1, 1).data;
      return [data?.[0] ?? -1, data?.[1] ?? -1, data?.[2] ?? -1];
    },
    [x, y],
  );
}

/** clickAt clicks the canvas where the machine's screen point (x, y) is shown. */
async function clickAt(page: Page, canvas: Locator, x: number, y: number) {
  // At 1:1 the screen can be taller than what's left of the window.
  await canvas.scrollIntoViewIfNeeded();
  const box = await canvas.boundingBox();
  const width = await canvas.evaluate((element: HTMLCanvasElement) => element.width);
  const height = await canvas.evaluate((element: HTMLCanvasElement) => element.height);
  if (box === null) {
    throw new Error('the screen isn’t shown');
  }
  await page.mouse.click(
    box.x + ((x + 0.5) * box.width) / width,
    box.y + ((y + 0.5) * box.height) / height,
  );
}

const hasXvfb = spawnSync('Xvfb', ['-help'], { stdio: 'ignore' }).error === undefined;

test('M8: an X11 desktop, seen and clicked on, directly and through the server', async ({
  page,
}, testInfo) => {
  test.skip(!hasXvfb, 'Xvfb isn’t installed');
  test.setTimeout(120_000);
  const display = new XDisplay();
  await display.start(join('test-results', 'e2e-x11', testInfo.project.name));
  display.paint();
  const server = new OwnServer(
    x11Ports[testInfo.project.name] ?? 7584,
    `x11-${testInfo.project.name}`,
  );
  const agent = new Agent(`x11-${testInfo.project.name}`);
  agent.writePolicy(`[desktop]\ndisplay = "${display.name}"\n`);
  await server.start();
  try {
    await signInAsNewAdmin(page, server);
    await page.goto(`${server.origin}/machines/add`);
    await page.getByRole('button', { name: 'Create install command' }).click();
    agent.enroll(agentFlags(await page.locator('code').first().innerText()));
    agent.run();
    await page.goto(`${server.origin}/`);
    await openOnlineMachine(page);

    await page.getByRole('link', { name: 'Desktop' }).click();
    const desktop = page.getByRole('region', { name: 'Remote desktop' });
    await expect(desktop.getByRole('status')).toHaveText(`Direct (WebRTC) · X11 ${display.name}`, {
      timeout: 20_000,
    });
    const canvas = desktop.getByLabel('Screen');
    await expect(canvas).toHaveJSProperty('width', 800);
    // The red square xprobe painted, on Xvfb's white background.
    const isRed = async () => {
      const [r = 0, g = 255, b = 255] = await pixel(canvas, 130, 130);
      return r > 240 && g < 20 && b < 20;
    };
    await expect.poll(isRed, { timeout: 15_000 }).toBe(true);
    await clickAt(page, canvas, 400, 300);
    await expect.poll(() => display.pointer()).toMatchObject({ x: 400, y: 300 });

    await desktop.getByRole('checkbox', { name: 'Relay through the server' }).check();
    await expect(desktop.getByRole('status')).toHaveText(
      `Through the server · X11 ${display.name}`,
      {
        timeout: 20_000,
      },
    );
    await expect(canvas).toHaveJSProperty('width', 800);
    await clickAt(page, canvas, 600, 450);
    await expect.poll(() => display.pointer()).toMatchObject({ x: 600, y: 450 });
  } catch (error) {
    await testInfo.attach('agent log', { body: agent.log });
    console.log(agent.log);
    throw error;
  } finally {
    agent.stop();
    display.stop();
    await server.stop();
  }
});
