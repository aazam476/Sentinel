// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';
import { afterEach, vi } from 'vitest';

import { findAccessibilityViolations } from '../../test/axe';
import { installFakeWebAuthn, passkeyOptions } from '../../test/fakeWebAuthn';
import { adminSession, passkeys, sessions } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

afterEach(() => {
  vi.unstubAllGlobals();
});

function mockAccount() {
  let currentPasskeys = passkeys;
  server.use(
    http.get(apiPath('auth/passkeys'), () => json({ passkeys: currentPasskeys })),
    http.get(apiPath('auth/sessions'), () => json({ sessions })),
  );
  return {
    removeAll: () => {
      currentPasskeys = [];
    },
  };
}

describe('account page', () => {
  it('SEC-06 lists sessions, marking this browser', async () => {
    signedInAs(adminSession);
    mockAccount();
    renderApp('/account');

    expect(await screen.findByText('Safari on macOS')).toBeVisible();
    expect(screen.getByText('This browser')).toBeVisible();
    expect(screen.getByText(/198\.51\.100\.20/)).toBeVisible();
  });

  it('SEC-06 signs out another session', async () => {
    signedInAs(adminSession);
    mockAccount();
    let revoked: string | undefined;
    server.use(
      http.delete(apiPath('auth/sessions/:id'), ({ params }) => {
        revoked = params.id as string;
        return new Response(null, { status: 204 });
      }),
    );
    renderApp('/account');

    await userEvent.setup().click(
      await screen.findByRole('button', {
        name: 'Sign out Chrome on Windows from 198.51.100.20',
      }),
    );

    expect(await screen.findByText('Signed out Chrome on Windows')).toBeVisible();
    expect(revoked).toBe('11');
  });

  it('removes a passkey after confirmation', async () => {
    signedInAs(adminSession);
    const account = mockAccount();
    server.use(
      http.delete(apiPath('auth/passkeys/:id'), () => {
        account.removeAll();
        return new Response(null, { status: 204 });
      }),
    );
    renderApp('/account');
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Remove passkey Laptop' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Remove this passkey?' });
    await user.click(within(dialog).getByRole('button', { name: 'Remove' }));

    expect(await screen.findByText('No passkeys yet.')).toBeVisible();
    expect(screen.getByText('Removed “Laptop”')).toBeVisible();
  });

  it('SEC-07 asks for a fresh code when the server wants a step-up, then retries', async () => {
    installFakeWebAuthn();
    signedInAs(adminSession);
    mockAccount();
    let attempts = 0;
    let steppedUp = false;
    server.use(
      http.post(apiPath('auth/passkeys/registration/begin'), () => {
        attempts++;
        return steppedUp
          ? json(passkeyOptions)
          : apiError(403, 'step_up_required', 'confirm it’s you first');
      }),
      http.post(apiPath('auth/step-up'), async ({ request }) => {
        const body = (await request.json()) as { totpCode: string };
        if (body.totpCode !== '123456') {
          return apiError(401, 'invalid_code', 'the code is incorrect or has already been used');
        }
        steppedUp = true;
        return json(adminSession);
      }),
      http.post(
        apiPath('auth/passkeys/registration/finish'),
        () => new Response(null, { status: 204 }),
      ),
    );
    renderApp('/account');
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('New passkey name'), 'Phone');
    await user.click(screen.getByRole('button', { name: 'Add passkey' }));
    const dialog = await screen.findByRole('dialog', { name: 'Confirm it’s you' });
    await user.type(within(dialog).getByLabelText('Authenticator code'), '000000');
    await user.click(within(dialog).getByRole('button', { name: 'Confirm' }));
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The code is incorrect');

    await user.clear(within(dialog).getByLabelText('Authenticator code'));
    await user.type(within(dialog).getByLabelText('Authenticator code'), '123456');
    await user.click(within(dialog).getByRole('button', { name: 'Confirm' }));

    expect(await screen.findByText('Passkey added')).toBeVisible();
    expect(attempts).toBe(2);
  });

  it('closing the step-up dialog cancels the action', async () => {
    installFakeWebAuthn();
    signedInAs(adminSession);
    mockAccount();
    server.use(
      http.post(apiPath('auth/passkeys/registration/begin'), () =>
        apiError(403, 'step_up_required', 'confirm first'),
      ),
    );
    renderApp('/account');
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Add passkey' }));
    await user.click(await screen.findByRole('button', { name: 'Cancel' }));

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('offers a passkey in the step-up dialog to passkey users', async () => {
    const webAuthn = installFakeWebAuthn();
    signedInAs({ ...adminSession, secondFactors: { totp: false, passkey: true } });
    mockAccount();
    let steppedUp = false;
    server.use(
      http.post(apiPath('auth/passkeys/registration/begin'), () =>
        steppedUp ? json(passkeyOptions) : apiError(403, 'step_up_required', 'confirm first'),
      ),
      http.post(apiPath('auth/passkeys/assertion/begin'), () => json(passkeyOptions)),
      http.post(apiPath('auth/passkeys/assertion/finish'), () => {
        steppedUp = true;
        return json(adminSession);
      }),
      http.post(
        apiPath('auth/passkeys/registration/finish'),
        () => new Response(null, { status: 204 }),
      ),
    );
    renderApp('/account');
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Add passkey' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).queryByLabelText('Authenticator code')).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole('button', { name: 'Use a passkey' }));

    expect(await screen.findByText('Passkey added')).toBeVisible();
    expect(webAuthn.get).toHaveBeenCalledOnce();
  });

  it('has no accessibility violations', async () => {
    signedInAs(adminSession);
    mockAccount();
    const { container } = renderApp('/account');
    await screen.findByText('Safari on macOS');
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });
});
