// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type {
  CreateEnrollmentTokenRequest,
  CreateEnrollmentTokenResponse,
} from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { adminSession, enrollmentTokens } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const created: CreateEnrollmentTokenResponse = {
  token: {
    ...enrollmentTokens[0],
    id: 4,
    note: 'rack server',
    maxUses: 3,
  } as CreateEnrollmentTokenResponse['token'],
  secret: 'tok_9f8e7d6c5b4a',
  serverFingerprint: 'SHA256:serverKeyFingerprint',
  tlsFingerprint: 'SHA256:tlsKeyFingerprint',
};

function withTokens() {
  server.use(
    http.get(apiPath('enrollment-tokens'), () =>
      json({
        tokens: enrollmentTokens,
        serverFingerprint: created.serverFingerprint,
        tlsFingerprint: '',
      }),
    ),
  );
}

describe('add machine', () => {
  it('creates an install command with the token and both fingerprints', async () => {
    signedInAs(adminSession);
    withTokens();
    let sent: CreateEnrollmentTokenRequest | undefined;
    server.use(
      http.post(apiPath('enrollment-tokens'), async ({ request }) => {
        sent = (await request.json()) as CreateEnrollmentTokenRequest;
        return json(created, 201);
      }),
    );
    const { container } = renderApp('/machines/add');
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Note (optional)'), 'rack server');
    await user.selectOptions(screen.getByLabelText('Valid for'), '168');
    await user.clear(screen.getByLabelText('Machines it can add'));
    await user.type(screen.getByLabelText('Machines it can add'), '3');
    await user.click(screen.getByRole('button', { name: 'Create install command' }));

    expect(await screen.findByRole('heading', { name: 'Run this on the machine' })).toBeVisible();
    expect(sent).toEqual({ note: 'rack server', expiresInHours: 168, maxUses: 3 });
    const commands = screen.getAllByText(/sentinel-agent(\.exe)? install/);
    expect(commands).toHaveLength(2);
    for (const command of commands) {
      expect(command).toHaveTextContent('--token tok_9f8e7d6c5b4a');
      expect(command).toHaveTextContent('--fingerprint SHA256:serverKeyFingerprint');
      expect(command).toHaveTextContent('--tls-fingerprint SHA256:tlsKeyFingerprint');
    }
    expect(screen.getByText(/works for up to 3 machines/)).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('copies a command to the clipboard', async () => {
    signedInAs(adminSession);
    withTokens();
    server.use(http.post(apiPath('enrollment-tokens'), () => json(created, 201)));
    renderApp('/machines/add');
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Create install command' }));
    await user.click(
      await screen.findByRole('button', { name: 'Copy the Linux and macOS command' }),
    );

    expect(await navigator.clipboard.readText()).toMatch(/^sudo \.\/sentinel-agent install /);
    expect(await screen.findByText('Copied the Linux and macOS command')).toBeVisible();
  });

  it('says to copy by hand when the clipboard isn’t available', async () => {
    signedInAs(adminSession);
    withTokens();
    server.use(http.post(apiPath('enrollment-tokens'), () => json(created, 201)));
    renderApp('/machines/add');
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Create install command' }));
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('not allowed'));
    await user.click(await screen.findByRole('button', { name: 'Copy the Windows command' }));
    expect(await screen.findByText(/Couldn’t copy automatically/)).toBeVisible();
  });

  it('shows the server’s reason when a token can’t be created', async () => {
    signedInAs(adminSession);
    withTokens();
    server.use(
      http.post(apiPath('enrollment-tokens'), () =>
        apiError(400, 'invalid_input', 'A token can be used between 1 and 1,000 times.', {
          field: 'maxUses',
        }),
      ),
    );
    renderApp('/machines/add');
    await userEvent
      .setup()
      .click(await screen.findByRole('button', { name: 'Create install command' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('between 1 and 1,000 times');
  });

  it('lists recent commands with their state and cancels a usable one', async () => {
    signedInAs(adminSession);
    withTokens();
    let cancelled: string | undefined;
    server.use(
      http.delete(apiPath('enrollment-tokens/:id'), ({ params }) => {
        cancelled = String(params.id);
        return new Response(null, { status: 204 });
      }),
    );
    renderApp('/machines/add');
    const list = await screen.findByRole('list');
    expect(within(list).getByText(/Valid until/)).toBeVisible();
    expect(within(list).getByText(/Used up · used 2 of 2/)).toBeVisible();
    expect(within(list).getByText(/Cancelled/)).toBeVisible();
    expect(within(list).getAllByRole('button')).toHaveLength(1);

    await userEvent.setup().click(within(list).getByRole('button', { name: 'Cancel office NAS' }));
    expect(await screen.findByText('The install command no longer works')).toBeVisible();
    expect(cancelled).toBe('3');
  });

  it('says when there are no recent commands', async () => {
    signedInAs(adminSession);
    server.use(
      http.get(apiPath('enrollment-tokens'), () =>
        json({ tokens: [], serverFingerprint: 'x', tlsFingerprint: '' }),
      ),
    );
    renderApp('/machines/add');
    expect(await screen.findByText('None in the last week.')).toBeVisible();
  });
});
