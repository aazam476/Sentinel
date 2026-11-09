// SPDX-License-Identifier: Apache-2.0
import { expect, test, type Locator, type Page } from '@playwright/test';

import {
  expectAccessible,
  expectNoHorizontalScroll,
  openOnlineMachine,
  signInAsNewAdmin,
  watchForCSPViolations,
} from './helpers';
import { Agent, agentFlags, OwnServer } from './processes';

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
