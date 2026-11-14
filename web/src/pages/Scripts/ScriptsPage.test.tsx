// SPDX-License-Identifier: Apache-2.0
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';
import { vi } from 'vitest';

import type { Schedule, Script, ScriptBatchDetails } from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import {
  adminSession,
  agents,
  fixtureNow,
  viewerSession,
} from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';
import { describeCron, describeTarget } from './scriptText';

// CodeMirror needs a real browser's layout; the end-to-end test drives it.
vi.mock('../../components/CodeEditor/CodeEditor', () => ({
  CodeEditor: ({
    label,
    value,
    onChange,
  }: {
    label: string;
    value: string;
    onChange: (v: string) => void;
  }) => (
    <textarea
      aria-label={label}
      value={value}
      onChange={(event) => {
        onChange(event.target.value);
      }}
    />
  ),
}));

const uptime: Script = {
  id: 7,
  name: 'Uptime',
  description: 'How long each machine has been up',
  language: 'sh',
  body: 'uptime',
  targetOS: 'any',
  timeoutSeconds: 600,
  createdAt: fixtureNow,
  updatedAt: fixtureNow,
};

const nas = agents[0];

const batch: ScriptBatchDetails = {
  id: 3,
  scriptId: 7,
  scriptName: 'Uptime',
  startedBy: 'admin',
  scheduleId: null,
  target: 'tag homelab',
  startedAt: fixtureNow,
  counts: { succeeded: 1, failed: 1 },
  language: 'sh',
  body: 'uptime',
  runs: [
    {
      id: 30,
      batchId: 3,
      agentId: nas?.id ?? '',
      agentName: 'nas-01',
      state: 'succeeded',
      exitCode: 0,
      error: '',
      startedAt: fixtureNow,
      finishedAt: fixtureNow + 2_000,
    },
    {
      id: 31,
      batchId: 3,
      agentId: 'x',
      agentName: 'pc',
      state: 'failed',
      exitCode: 127,
      error: '',
      startedAt: fixtureNow,
      finishedAt: fixtureNow + 1_000,
    },
  ],
};

function withScripts() {
  signedInAs(adminSession);
  const sent: string[] = [];
  server.use(
    http.get(apiPath('agents'), () => json({ agents })),
    http.get(apiPath('scripts'), () => json({ scripts: [uptime] })),
    http.post(apiPath('scripts'), async ({ request }) => {
      sent.push(`create ${await request.text()}`);
      return json({ ...uptime, id: 8, name: 'Disk usage' });
    }),
    http.post(apiPath('scripts/7/run'), async ({ request }) => {
      sent.push(`run ${await request.text()}`);
      return json({ batchId: 3 });
    }),
    http.get(apiPath('script-batches/3'), () => json(batch)),
    http.get(apiPath('script-batches'), () => json({ batches: [batch] })),
    http.get(apiPath('script-runs/31'), () =>
      json({ ...batch.runs[1], output: 'sh: 1: zpool: not found\n', truncated: false }),
    ),
  );
  return sent;
}

describe('Scripts', () => {
  it('lists the library and writes a new script', async () => {
    const sent = withScripts();
    const user = userEvent.setup();
    const { container } = renderApp('/scripts');
    expect(await screen.findByRole('link', { name: 'Uptime' })).toBeVisible();
    expect(screen.getByText('How long each machine has been up')).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.click(screen.getByRole('link', { name: 'New script' }));
    await user.type(await screen.findByRole('textbox', { name: 'Name' }), 'Disk usage');
    await user.selectOptions(screen.getByRole('combobox', { name: 'Language' }), 'sh');
    const body = screen.getByRole('textbox', { name: 'Script' });
    await user.clear(body);
    await user.type(body, 'df -h');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(sent).toEqual([
        'create {"name":"Disk usage","description":"","language":"sh","body":"df -h","targetOS":"any","timeoutSeconds":600}',
      ]);
    });
  });

  it('runs a script on a tag and opens the run', async () => {
    const sent = withScripts();
    const user = userEvent.setup();
    renderApp('/scripts/7');
    await user.click(await screen.findByRole('button', { name: 'Run…' }));
    const dialog = await screen.findByRole('dialog', { name: 'Run Uptime' });
    await user.click(within(dialog).getByRole('radio', { name: 'Machines with a tag' }));
    await user.selectOptions(within(dialog).getByRole('combobox', { name: 'Tag' }), 'homelab');
    await user.click(within(dialog).getByRole('button', { name: 'Run' }));
    expect(await screen.findByRole('region', { name: 'Run of Uptime' })).toBeVisible();
    expect(sent).toEqual(['run {"target":{"kind":"tag","value":"homelab"}}']);
  });

  it('shows each machine’s result, and a run’s output when opened', async () => {
    withScripts();
    const user = userEvent.setup();
    const { container } = renderApp('/scripts/runs/3');
    const pc = await screen.findByRole('listitem', { name: 'pc' });
    expect(within(pc).getByText('Failed')).toBeVisible();
    expect(within(pc).getByText('exit 127')).toBeVisible();
    await user.click(within(pc).getByRole('button', { name: 'pc' }));
    expect(await within(pc).findByText('sh: 1: zpool: not found')).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('lists runs with how many ended how', async () => {
    withScripts();
    renderApp('/scripts/runs');
    const row = await screen.findByRole('listitem', { name: /^Uptime,/ });
    expect(within(row).getByText('tag homelab')).toBeVisible();
    expect(within(row).getByText('by admin')).toBeVisible();
    expect(within(row).getByText('Succeeded')).toBeVisible();
  });

  it('schedules a script, describing the cron in words, and shows the server’s objection', async () => {
    withScripts();
    const user = userEvent.setup();
    const saved: Schedule = {
      id: 1,
      scriptId: 7,
      scriptName: 'Uptime',
      cron: '0 3 * * 1-5',
      timezone: 'UTC',
      target: { kind: 'tag', value: 'homelab' },
      enabled: true,
      nextRunAt: fixtureNow + 3_600_000,
      lastRunAt: null,
    };
    let schedules: Schedule[] = [];
    server.use(
      http.get(apiPath('schedules'), () => json({ schedules })),
      http.post(apiPath('schedules'), async ({ request }) => {
        const body = (await request.json()) as { cron: string };
        if (body.cron === '0 3 * * *') {
          return apiError(400, 'invalid_input', 'that schedule never comes round');
        }
        schedules = [saved];
        return json(saved);
      }),
    );
    renderApp('/scripts/schedules');
    expect(await screen.findByText('Nothing is scheduled.')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'New schedule' }));
    const dialog = await screen.findByRole('dialog', { name: 'New schedule' });
    expect(within(dialog).getByText('At 03:00')).toBeVisible();
    await user.selectOptions(within(dialog).getByRole('combobox', { name: 'Tag' }), 'homelab');
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(await within(dialog).findByText('That schedule never comes round.')).toBeVisible();
    const cron = within(dialog).getByRole('textbox', { name: 'When (cron)' });
    await user.clear(cron);
    await user.type(cron, '0 3 * * 1-5');
    expect(within(dialog).getByText('At 03:00, Monday through Friday')).toBeVisible();
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    const row = await screen.findByRole('listitem', { name: 'Schedule of Uptime' });
    expect(within(row).getByText('At 03:00, Monday through Friday')).toBeVisible();
    expect(within(row).getByText('Machines tagged homelab')).toBeVisible();
  });

  it('changes a schedule, sending only what a save takes', async () => {
    withScripts();
    const user = userEvent.setup();
    const existing: Schedule = {
      id: 4,
      scriptId: 7,
      scriptName: 'Uptime',
      cron: '@daily',
      timezone: 'UTC',
      target: { kind: 'tag', value: 'homelab' },
      enabled: true,
      nextRunAt: fixtureNow,
      lastRunAt: null,
    };
    const sent: string[] = [];
    server.use(
      http.get(apiPath('schedules'), () => json({ schedules: [existing] })),
      http.put(apiPath('schedules/4'), async ({ request }) => {
        sent.push(await request.text());
        return json({ ...existing, enabled: false });
      }),
    );
    renderApp('/scripts/schedules');
    await user.click(await screen.findByRole('button', { name: 'Uptime' }));
    const dialog = await screen.findByRole('dialog', { name: 'Change schedule' });
    await user.click(within(dialog).getByRole('checkbox', { name: /^On/ }));
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(sent).toEqual([
        '{"scriptId":7,"cron":"@daily","timezone":"UTC","target":{"kind":"tag","value":"homelab"},"enabled":false}',
      ]);
    });
  });

  it('SEC-07: asks to confirm it’s you before running on several machines, then runs', async () => {
    withScripts();
    const user = userEvent.setup();
    let attempts = 0;
    server.use(
      http.post(apiPath('scripts/7/run'), () => {
        attempts++;
        return attempts === 1
          ? apiError(403, 'step_up_required', 'confirm it’s you first')
          : json({ batchId: 3 });
      }),
      http.post(apiPath('auth/step-up'), () => json(adminSession)),
    );
    renderApp('/scripts/7');
    await user.click(await screen.findByRole('button', { name: 'Run…' }));
    const dialog = await screen.findByRole('dialog', { name: 'Run Uptime' });
    await user.click(within(dialog).getByRole('radio', { name: 'Machines with a tag' }));
    await user.selectOptions(within(dialog).getByRole('combobox', { name: 'Tag' }), 'homelab');
    await user.click(within(dialog).getByRole('button', { name: 'Run' }));

    const stepUp = await screen.findByRole('dialog', { name: /Confirm/ });
    // The step-up dialog asks; the refusal isn't also shown as an error.
    expect(screen.queryByText(/confirm it’s you first/i)).not.toBeInTheDocument();
    await user.type(within(stepUp).getByRole('textbox'), '123456');
    await user.click(within(stepUp).getByRole('button', { name: /Confirm/ }));
    expect(await screen.findByRole('region', { name: 'Run of Uptime' })).toBeVisible();
    expect(attempts).toBe(2);
  }, 20_000);

  it('SEC-07: asks to confirm it’s you before saving a schedule on a tag, then saves', async () => {
    withScripts();
    const user = userEvent.setup();
    const saved: Schedule = {
      id: 1,
      scriptId: 7,
      scriptName: 'Uptime',
      cron: '0 3 * * *',
      timezone: 'UTC',
      target: { kind: 'tag', value: 'homelab' },
      enabled: true,
      nextRunAt: fixtureNow + 3_600_000,
      lastRunAt: null,
    };
    let schedules: Schedule[] = [];
    let attempts = 0;
    server.use(
      http.get(apiPath('schedules'), () => json({ schedules })),
      http.post(apiPath('schedules'), () => {
        attempts++;
        if (attempts === 1) {
          return apiError(403, 'step_up_required', 'confirm it’s you first');
        }
        schedules = [saved];
        return json(saved);
      }),
      http.post(apiPath('auth/step-up'), () => json(adminSession)),
    );
    renderApp('/scripts/schedules');
    await user.click(await screen.findByRole('button', { name: 'New schedule' }));
    const dialog = await screen.findByRole('dialog', { name: 'New schedule' });
    await user.selectOptions(within(dialog).getByRole('combobox', { name: 'Tag' }), 'homelab');
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));

    const stepUp = await screen.findByRole('dialog', { name: /Confirm/ });
    await user.type(within(stepUp).getByRole('textbox'), '123456');
    await user.click(within(stepUp).getByRole('button', { name: /Confirm/ }));
    expect(await screen.findByRole('listitem', { name: 'Schedule of Uptime' })).toBeVisible();
    expect(await screen.findByText('Schedule saved')).toBeVisible();
    expect(attempts).toBe(2);
  }, 20_000);

  it('SEC-07: asks to confirm it’s you before changing a script a schedule runs widely, then saves', async () => {
    withScripts();
    const user = userEvent.setup();
    const sent: string[] = [];
    server.use(
      http.put(apiPath('scripts/7'), async ({ request }) => {
        sent.push(await request.text());
        return sent.length === 1
          ? apiError(403, 'step_up_required', 'confirm it’s you first')
          : json({ ...uptime, description: 'Changed' });
      }),
      http.post(apiPath('auth/step-up'), () => json(adminSession)),
    );
    renderApp('/scripts/7');
    const description = await screen.findByRole('textbox', { name: 'Description' });
    await user.clear(description);
    await user.type(description, 'Changed');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    const stepUp = await screen.findByRole('dialog', { name: /Confirm/ });
    expect(screen.queryByText(/confirm it’s you first/i)).not.toBeInTheDocument();
    await user.type(within(stepUp).getByRole('textbox'), '123456');
    await user.click(within(stepUp).getByRole('button', { name: /Confirm/ }));
    expect(await screen.findByText('Saved Uptime')).toBeVisible();
    // The same change is sent again, not a stale or empty one.
    expect(sent).toHaveLength(2);
    expect(sent[1]).toBe(sent[0]);
    expect(sent[1]).toContain('"description":"Changed"');
  }, 20_000);

  it('tells viewers scripts aren’t theirs', async () => {
    signedInAs(viewerSession);
    renderApp('/scripts');
    expect(await screen.findByText(/viewers can’t see them/)).toBeVisible();
  });
});

describe('script text', () => {
  it.each([
    ['*/15 * * * *', 'Every 15 minutes'],
    ['0 3 * * 1-5', 'At 03:00, Monday through Friday'],
    ['not a cron', undefined],
  ])('describes %s', (cron, want) => {
    expect(describeCron(cron)).toBe(want);
  });

  it('names a target', () => {
    const names = new Map([['a', 'nas']]);
    expect(describeTarget({ kind: 'agent', value: 'a' }, names)).toBe('nas');
    expect(describeTarget({ kind: 'agents', value: 'a,b,c' }, names)).toBe('3 machines');
    expect(describeTarget({ kind: 'tag', value: 'lab' }, names)).toBe('Machines tagged lab');
  });
});
