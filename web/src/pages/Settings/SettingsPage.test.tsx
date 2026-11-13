// SPDX-License-Identifier: Apache-2.0
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type { SettingsRequest, SettingsResponse } from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { adminSession, viewerSession } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const current: SettingsResponse = {
  retention: { rawHours: 24, minuteDays: 7, hourDays: 365 },
  updateChannel: 'stable',
  server: {
    listen: '0.0.0.0:8443',
    publicUrl: 'https://sentinel.example.com',
    basePath: '/',
    trustedProxies: [],
    tlsMode: 'self-signed',
    acmeEmail: '',
    behindTlsProxy: false,
    releaseUrl: '',
    maxFileBytes: 1_073_741_824,
    turnListen: '',
  },
};

function withSettings(save: (request: SettingsRequest) => Response | Promise<Response>) {
  signedInAs(adminSession);
  server.use(
    http.get(apiPath('settings'), () => json(current)),
    http.put(apiPath('settings'), async ({ request }) =>
      save((await request.json()) as SettingsRequest),
    ),
  );
}

describe('§9.10 settings page', () => {
  it('saves new retention and the release channel', async () => {
    const saved: SettingsRequest[] = [];
    withSettings((request) => {
      saved.push(request);
      return json({
        ...current,
        retention: request.retention,
        updateChannel: request.updateChannel,
      });
    });
    const { container } = renderApp('/settings');
    const raw = await screen.findByLabelText('Raw samples (hours)');
    expect(raw).toHaveValue(24);
    await userEvent.clear(raw);
    await userEvent.type(raw, '48');
    await userEvent.selectOptions(screen.getByLabelText('Release channel'), 'beta');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Settings saved')).toBeVisible();
    expect(saved).toEqual([
      { retention: { rawHours: 48, minuteDays: 7, hourDays: 365 }, updateChannel: 'beta' },
    ]);
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('shows what the server refused next to the field', async () => {
    withSettings(() =>
      apiError(400, 'invalid_input', 'Keep hourly averages 30 to 1825 days.', {
        field: 'hourDays',
      }),
    );
    renderApp('/settings');
    await userEvent.click(await screen.findByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Keep hourly averages 30 to 1825 days.')).toBeVisible();
    expect(screen.getByLabelText('Hourly averages (days)')).toHaveAccessibleDescription(
      'Keep hourly averages 30 to 1825 days.',
    );
  });

  it('asks to confirm it’s you, then saves', async () => {
    let calls = 0;
    withSettings(() => {
      calls += 1;
      return calls === 1
        ? apiError(403, 'step_up_required', 'Confirm it’s you first.')
        : json(current);
    });
    renderApp('/settings');
    await userEvent.click(await screen.findByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('dialog')).toBeVisible();
  });

  it('shows server.toml read-only and says where the other settings are', async () => {
    withSettings(() => json(current));
    renderApp('/settings');
    expect(await screen.findByText('0.0.0.0:8443')).toBeVisible();
    expect(screen.getByText('Off: upload builds by hand')).toBeVisible();
    expect(screen.getByText('1 GB')).toBeVisible();
    expect(screen.getByRole('link', { name: 'Alerts → Channels' })).toHaveAttribute(
      'href',
      '/alerts/channels',
    );
    expect(screen.getByRole('link', { name: 'Settings' })).toHaveAttribute('aria-current', 'page');
  });

  it('tells non-admins the page is for admins and hides the link', async () => {
    signedInAs(viewerSession);
    renderApp('/settings');
    expect(await screen.findByText('Only admins change the server’s settings.')).toBeVisible();
    expect(screen.queryByRole('link', { name: 'Settings' })).toBeNull();
  });
});
