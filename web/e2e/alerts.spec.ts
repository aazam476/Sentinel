// SPDX-License-Identifier: Apache-2.0
import { expect, type Page, test } from '@playwright/test';
import { createServer as createHTTPServer, type Server } from 'node:http';
import { createServer as createTCPServer, type Server as TCPServer } from 'node:net';

import { expectNoHorizontalScroll, signInAsNewAdmin, watchForCSPViolations } from './helpers';
import { Agent, agentFlags, OwnServer } from './processes';

/**
 * M6 "Done when", for alerts: a CPU threshold rule, made in the UI, fires
 * on the real agent's readings and then resolves, with a notification on
 * each of the four channels for both. The channels point at local stand-ins
 * (a webhook receiver, an ntfy server, a Discord webhook and an SMTP
 * server), so nothing leaves the machine. Rules are evaluated once a
 * minute, so this waits up to a few minutes.
 */
const ports: Record<string, number> = {
  desktop: 7540,
  phone: 7541,
  'desktop-reduced-motion': 7542,
  'phone-reduced-motion': 7543,
};

/** Received is what one stand-in got: the path and body of each request or email. */
interface Received {
  path: string;
  headers: Partial<Record<string, string>>;
  body: string;
}

/** standIns starts the HTTP and SMTP stand-ins, recording everything sent to them. */
async function standIns() {
  const received: Received[] = [];
  const http: Server = createHTTPServer((request, response) => {
    let body = '';
    request.setEncoding('utf8');
    request.on('data', (chunk: string) => {
      body += chunk;
    });
    request.on('end', () => {
      const headers = Object.fromEntries(
        Object.entries(request.headers).map(([name, value]) => [name, String(value)]),
      );
      received.push({ path: request.url ?? '', headers, body });
      response.writeHead(200, { 'Content-Type': 'application/json' }).end('{}');
    });
  });
  const smtp: TCPServer = createTCPServer((socket) => {
    socket.setEncoding('utf8');
    socket.write('220 mail.test ESMTP\r\n');
    let buffer = '';
    let message: string | undefined;
    socket.on('data', (chunk: string) => {
      buffer += chunk;
      let end = buffer.indexOf('\r\n');
      while (end !== -1) {
        const line = buffer.slice(0, end);
        buffer = buffer.slice(end + 2);
        if (message !== undefined) {
          if (line === '.') {
            received.push({ path: 'smtp', headers: {}, body: message });
            message = undefined;
            socket.write('250 queued\r\n');
          } else {
            message += `${line}\n`;
          }
        } else {
          const command = line.split(' ')[0]?.toUpperCase();
          if (command === 'EHLO') {
            socket.write('250 mail.test\r\n');
          } else if (command === 'DATA') {
            message = '';
            socket.write('354 go ahead\r\n');
          } else if (command === 'QUIT') {
            socket.end('221 bye\r\n');
          } else {
            socket.write('250 ok\r\n');
          }
        }
        end = buffer.indexOf('\r\n');
      }
    });
  });
  await new Promise<void>((resolve) => http.listen(0, '127.0.0.1', resolve));
  await new Promise<void>((resolve) => smtp.listen(0, '127.0.0.1', resolve));
  const port = (server: Server | TCPServer) => {
    const address = server.address();
    return typeof address === 'object' && address !== null ? address.port : 0;
  };
  return {
    received,
    url: `http://127.0.0.1:${String(port(http))}`,
    smtpPort: port(smtp),
    close: () => {
      http.close();
      smtp.close();
    },
  };
}

/** addChannel adds a notification channel through the dialog. */
async function addChannel(
  page: Page,
  name: string,
  type: string,
  fill: (page: Page) => Promise<void>,
) {
  await page.getByRole('button', { name: 'New channel' }).click();
  const dialog = page.getByRole('dialog', { name: 'New channel' });
  await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  await dialog.getByRole('combobox', { name: 'Type' }).selectOption(type);
  await fill(page);
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('listitem', { name: `Channel ${name}` })).toBeVisible();
}

/** notifications lists which channel was told what, as "path event". */
function notifications(received: Received[]): string[] {
  return received
    .map((item) => {
      if (item.path === 'smtp') {
        const subject = /^Subject: (.*)$/m.exec(item.body)?.[1] ?? '';
        return `email ${subject.includes('Resolved') ? 'resolved' : 'firing'}`;
      }
      if (item.path === '/hook') {
        return `webhook ${(JSON.parse(item.body) as { event: string }).event}`;
      }
      if (item.path === '/discord') {
        const content = (JSON.parse(item.body) as { content: string }).content;
        return `discord ${content.startsWith('Resolved') ? 'resolved' : 'firing'}`;
      }
      return `ntfy ${(item.headers.title ?? '').startsWith('Resolved') ? 'resolved' : 'firing'}`;
    })
    .sort();
}

test('M6: a CPU threshold alert fires and resolves, with notifications on each channel', async ({
  page,
}, testInfo) => {
  test.setTimeout(420_000);
  const stand = await standIns();
  const server = new OwnServer(
    ports[testInfo.project.name] ?? 7544,
    `alerts-${testInfo.project.name}`,
  );
  const agent = new Agent(`alerts-${testInfo.project.name}`);
  await server.start();
  try {
    const cspViolations = watchForCSPViolations(page);
    await signInAsNewAdmin(page, server);
    await page.goto(`${server.origin}/machines/add`);
    await page.getByRole('button', { name: 'Create install command' }).click();
    agent.enroll(agentFlags(await page.locator('code').first().innerText()));
    agent.run();
    await page.goto(`${server.origin}/`);
    await expect(page.getByRole('link', { name: /Online/ })).toBeVisible({ timeout: 15_000 });

    // The four channels, made in the UI.
    await page
      .getByRole('navigation', { name: 'Main' })
      .getByRole('link', { name: 'Alerts' })
      .click();
    await page.getByRole('link', { name: 'Channels' }).click();
    await addChannel(page, 'Ops email', 'email', async () => {
      const dialog = page.getByRole('dialog');
      await dialog.getByRole('textbox', { name: 'Mail server' }).fill('127.0.0.1');
      await dialog.getByRole('combobox', { name: 'Security' }).selectOption('none');
      await dialog.getByRole('spinbutton', { name: 'Port' }).fill(String(stand.smtpPort));
      await dialog.getByRole('textbox', { name: 'From', exact: true }).fill('sentinel@example.com');
      await dialog.getByRole('textbox', { name: 'To', exact: true }).fill('ops@example.com');
    });
    await addChannel(page, 'Ops webhook', 'webhook', async () => {
      await page
        .getByRole('dialog')
        .getByRole('textbox', { name: 'URL' })
        .fill(`${stand.url}/hook`);
    });
    await addChannel(page, 'Phone', 'ntfy', async () => {
      const dialog = page.getByRole('dialog');
      await dialog.getByRole('textbox', { name: 'Server', exact: true }).fill(stand.url);
      await dialog.getByRole('textbox', { name: 'Topic' }).fill('homelab');
    });
    await addChannel(page, 'Chat', 'discord', async () => {
      await page
        .getByRole('dialog')
        .getByRole('textbox', { name: 'Discord webhook URL' })
        .fill(`${stand.url}/discord`);
    });
    // A test through one of them, from the UI.
    await page
      .getByRole('listitem', { name: 'Channel Ops webhook' })
      .getByRole('button', { name: 'Send test' })
      .click();
    await expect(page.getByText('Sent a test to Ops webhook')).toBeVisible();
    await expect.poll(() => notifications(stand.received)).toEqual(['webhook test']);
    stand.received.length = 0;

    // A CPU rule that holds on any machine (CPU is never above 101%).
    await page.getByRole('link', { name: 'Rules' }).click();
    await page.getByRole('button', { name: 'New rule' }).click();
    const dialog = page.getByRole('dialog', { name: 'New alert rule' });
    await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill('CPU check');
    await dialog.getByRole('combobox', { name: 'Is' }).selectOption('below');
    await dialog.getByRole('spinbutton', { name: 'Threshold' }).fill('101');
    await dialog.getByRole('spinbutton', { name: 'For minutes' }).fill('1');
    await expect(dialog.getByText('CPU below 101% for 1 minute')).toBeVisible();
    await dialog.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByRole('listitem', { name: 'Rule CPU check' })).toContainText(
      'Notifies Chat, Ops email, Ops webhook, Phone',
    );

    // It fires, and every channel hears about it.
    await page
      .getByRole('navigation', { name: 'Alert sections' })
      .getByRole('link', { name: 'Alerts' })
      .click();
    const firing = page.getByRole('list', { name: 'Firing alerts' });
    await expect(firing.getByText(/^CPU has been below 101% for 1 minute /)).toBeVisible({
      timeout: 180_000,
    });
    await expect
      .poll(() => notifications(stand.received), { timeout: 30_000 })
      .toEqual(['discord firing', 'email firing', 'ntfy firing', 'webhook firing']);
    await expectNoHorizontalScroll(page);
    await firing.getByRole('button', { name: 'Acknowledge' }).click();
    await expect(firing.getByText(/Acknowledged by/)).toBeVisible();

    // Turned round so it can't hold, it resolves, and every channel hears that too.
    await page.getByRole('link', { name: 'Rules' }).click();
    await page.getByRole('button', { name: 'CPU check' }).click();
    const change = page.getByRole('dialog', { name: 'Change alert rule' });
    await change.getByRole('combobox', { name: 'Is' }).selectOption('above');
    await change.getByRole('button', { name: 'Save' }).click();
    await expect(change).toBeHidden();
    await page
      .getByRole('navigation', { name: 'Alert sections' })
      .getByRole('link', { name: 'Alerts' })
      .click();
    await expect(
      page.getByRole('list', { name: 'Resolved alerts' }).getByText(/^CPU has been below 101%/),
    ).toBeVisible({ timeout: 120_000 });
    await expect
      .poll(() => notifications(stand.received), { timeout: 30_000 })
      .toEqual([
        'discord firing',
        'discord resolved',
        'email firing',
        'email resolved',
        'ntfy firing',
        'ntfy resolved',
        'webhook firing',
        'webhook resolved',
      ]);

    // Every change is in the audit log.
    const audit = await page.request.get(`${server.origin}/api/audit?limit=100`);
    const actions = ((await audit.json()) as { entries: { action: string }[] }).entries.map(
      (entry) => entry.action,
    );
    for (const action of ['alert.channel.save', 'alert.rule.save', 'alert.acknowledge']) {
      expect(actions).toContain(action);
    }
    expect(cspViolations).toEqual([]);
  } finally {
    agent.stop();
    await server.stop();
    stand.close();
  }
});
