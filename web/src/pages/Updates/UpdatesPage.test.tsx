// SPDX-License-Identifier: Apache-2.0
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';

import type { AgentBuilds, AgentDetails } from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import {
  adminSession,
  fixtureNow,
  nasDetails,
  viewerSession,
} from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const builds: AgentBuilds = {
  builds: [
    {
      version: 'v0.2.0',
      os: 'linux',
      arch: 'amd64',
      file: 'sentinel-agent_v0.2.0_linux_amd64',
      size: 21_000_000,
      addedAt: fixtureNow,
    },
    {
      version: 'v0.2.0',
      os: 'windows',
      arch: 'amd64',
      file: 'sentinel-agent_v0.2.0_windows_amd64.exe',
      size: 22_000_000,
      addedAt: fixtureNow,
    },
  ],
  autoUpdate: false,
  canFetch: true,
  developmentKey: false,
};

function withBuilds(overrides: Partial<AgentBuilds> = {}) {
  signedInAs(adminSession);
  const sent: string[] = [];
  let current = { ...builds, ...overrides };
  server.use(
    http.get(apiPath('agent-builds'), () => json(current)),
    http.put(apiPath('agent-builds/auto-update'), async ({ request }) => {
      const body = (await request.json()) as { enabled: boolean };
      sent.push(`auto ${String(body.enabled)}`);
      current = { ...current, autoUpdate: body.enabled };
      return new HttpResponse(null, { status: 204 });
    }),
    http.post(apiPath('agent-updates'), async ({ request }) => {
      sent.push(`update ${await request.text()}`);
      return json({ started: ['a', 'b'], skipped: { c: 'it is offline' } });
    }),
    http.post(apiPath('agent-builds/fetch'), () => {
      sent.push('fetch');
      return json({ builds: builds.builds });
    }),
  );
  return sent;
}

describe('Agent updates', () => {
  it('lists the builds, turns automatic updates on, and updates every machine after confirming', async () => {
    const sent = withBuilds();
    const user = userEvent.setup();
    const { container } = renderApp('/updates');
    const list = await screen.findByRole('region', { name: 'Agent builds' });
    expect(
      await within(list).findByRole('listitem', { name: 'sentinel-agent_v0.2.0_linux_amd64' }),
    ).toHaveTextContent('linux/amd64');
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.click(screen.getByRole('checkbox', { name: /Update machines automatically/ }));
    expect(await screen.findByText('Machines will update by themselves')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Update all machines' }));
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Update' }),
    );
    expect(await screen.findByText('2 machines are updating; 1 was skipped')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Fetch the latest release' }));
    expect(await screen.findByText('Fetched 2 builds')).toBeVisible();
    expect(sent).toEqual([
      'auto true',
      'update {"target":{"kind":"all","value":""},"version":""}',
      'fetch',
    ]);
  });

  it('uploads a build with its signature as a form, and shows a refusal', async () => {
    withBuilds({ builds: [], canFetch: false });
    const user = userEvent.setup();
    const forms: FormData[] = [];
    server.use(
      http.post(apiPath('agent-builds'), async ({ request }) => {
        forms.push(await request.formData());
        if (forms.length === 1) {
          return apiError(400, 'invalid_input', 'the build’s release signature doesn’t check out', {
            field: 'signature',
          });
        }
        return json(builds.builds[0], 201);
      }),
    );
    renderApp('/updates');
    expect(
      await screen.findByText('No builds yet. Upload one, or fetch the latest release.'),
    ).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Fetch the latest release' })).toBeNull();
    const upload = screen.getByRole('region', { name: 'Upload a build' });
    await user.upload(
      within(upload).getByLabelText('Agent binary'),
      new File(['ELF'], 'sentinel-agent'),
    );
    await user.upload(
      within(upload).getByLabelText('Signature (.sig)'),
      new File(['sig'], 'sentinel-agent.sig'),
    );
    await user.click(within(upload).getByRole('button', { name: 'Upload' }));
    expect(
      await within(upload).findByText('The build’s release signature doesn’t check out.'),
    ).toBeVisible();
    await user.click(within(upload).getByRole('button', { name: 'Upload' }));
    expect(await screen.findByText('Added v0.2.0 for linux/amd64')).toBeVisible();
    expect([...(forms[0]?.keys() ?? [])]).toEqual(['signature', 'build']);
  }, 20_000);

  it('warns when the server trusts only the development key', async () => {
    withBuilds({ developmentKey: true });
    renderApp('/updates');
    expect(await screen.findByRole('note')).toHaveTextContent('development release key');
  });

  it('tells non-admins they can’t', async () => {
    signedInAs(viewerSession);
    renderApp('/updates');
    expect(await screen.findByText('Only admins update agents.')).toBeVisible();
    expect(screen.queryByRole('link', { name: 'Updates' })).toBeNull();
  });
});

describe('a machine’s agent', () => {
  function withMachine(details: AgentDetails) {
    signedInAs(adminSession);
    const sent: string[] = [];
    server.use(
      http.get(apiPath(`agents/${details.id}`), () => json(details)),
      http.get(apiPath(`agents/${details.id}/alerts`), () => json({ alerts: [] })),
      http.get(apiPath(`agents/${details.id}/metrics`), () =>
        json({ metric: 'cpu.percent', resolution: 'raw', points: [] }),
      ),
      http.get(apiPath('agent-builds'), () => json(builds)),
      http.post(apiPath('agent-updates'), async ({ request }) => {
        sent.push(await request.text());
        return json({ started: [details.id], skipped: {} });
      }),
    );
    return sent;
  }

  it('shows a rolled-back update and updates to the newest build for its system', async () => {
    const sent = withMachine({
      ...nasDetails,
      agentVersion: 'v0.1.0',
      update: {
        version: 'v0.2.0',
        state: 'rolled_back',
        error: 'v0.2.0 didn’t connect within 5m0s',
        startedAt: fixtureNow,
        finishedAt: fixtureNow,
      },
    });
    const user = userEvent.setup();
    renderApp(`/machines/${nasDetails.id}`);
    const panel = await screen.findByRole('region', { name: 'Agent' });
    expect(within(panel).getByText('Running v0.1.0.')).toBeVisible();
    expect(within(panel).getByText(/Went back after trying v0.2.0/)).toBeVisible();
    await user.click(await within(panel).findByRole('button', { name: 'Update to v0.2.0' }));
    await waitFor(() => {
      expect(sent).toEqual([
        `{"target":{"kind":"agent","value":"${nasDetails.id}"},"version":"v0.2.0"}`,
      ]);
    });
  });

  it('offers no update when the machine runs the newest build', async () => {
    withMachine({ ...nasDetails, agentVersion: 'v0.2.0' });
    renderApp(`/machines/${nasDetails.id}`);
    const panel = await screen.findByRole('region', { name: 'Agent' });
    expect(within(panel).getByText('Running v0.2.0.')).toBeVisible();
    expect(within(panel).queryByRole('button')).toBeNull();
  });
});
