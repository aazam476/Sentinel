// SPDX-License-Identifier: Apache-2.0
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';
import { afterEach, vi } from 'vitest';

import type { SignedInResponse } from '../../api/types.gen';
import { installFakeWebAuthn, passkeyOptions } from '../../test/fakeWebAuthn';
import { adminSession, enrollingSession } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const signedIn: SignedInResponse = {
  session: { ...adminSession, user: enrollingSession.user },
  recoveryCodes: Array.from({ length: 10 }, (_, i) => `CODE-${String(i)}`),
  remainingRecoveryCodes: null,
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('SEC-04 second-factor enrollment', () => {
  it('a user without a second factor can only reach enrollment', async () => {
    signedInAs(enrollingSession);
    renderApp('/');
    expect(
      await screen.findByRole('heading', { name: 'Set up two-factor authentication' }),
    ).toBeVisible();
  });

  it('sets up an authenticator app and shows recovery codes', async () => {
    signedInAs(enrollingSession);
    server.use(
      http.post(apiPath('auth/totp/enrollment'), () =>
        json({
          secret: 'JBSWY3DPEHPK3PXP',
          url: 'otpauth://x',
          qrCode: 'data:image/png;base64,iVBORw0KGgo=',
        }),
      ),
      http.post(apiPath('auth/totp/enrollment/confirm'), () => json(signedIn)),
    );
    renderApp('/login/enroll');
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Use an authenticator app' }));
    await user.type(await screen.findByLabelText('Authenticator code'), '123456');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));

    expect(await screen.findByRole('list', { name: 'Recovery codes' })).toBeVisible();
    await user.click(screen.getByLabelText('I have saved these codes'));
    await user.click(screen.getByRole('button', { name: 'Continue' }));
    expect(await screen.findByRole('heading', { name: 'Machines' })).toBeVisible();
  });

  it('enrolls a passkey instead, named by the user', async () => {
    const webAuthn = installFakeWebAuthn();
    signedInAs(enrollingSession);
    let finishBody: unknown;
    server.use(
      http.post(apiPath('auth/passkeys/registration/begin'), () => json(passkeyOptions)),
      http.post(apiPath('auth/passkeys/registration/finish'), async ({ request }) => {
        finishBody = await request.json();
        return json(signedIn);
      }),
    );
    renderApp('/login/enroll');
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Name for this passkey'), 'Work laptop');
    await user.click(screen.getByRole('button', { name: 'Use a passkey' }));

    expect(await screen.findByRole('list', { name: 'Recovery codes' })).toBeVisible();
    expect(webAuthn.create).toHaveBeenCalledOnce();
    expect(finishBody).toMatchObject({
      name: 'Work laptop',
      credential: { id: 'Bw', response: { attestationObject: 'Ag' } },
    });
  });

  it('shows nothing when the person closes the passkey prompt', async () => {
    installFakeWebAuthn({ cancelled: true });
    signedInAs(enrollingSession);
    server.use(http.post(apiPath('auth/passkeys/registration/begin'), () => json(passkeyOptions)));
    renderApp('/login/enroll');
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Use a passkey' }));

    expect(await screen.findByRole('button', { name: 'Use a passkey' })).toBeEnabled();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows a failure to start TOTP setup', async () => {
    signedInAs(enrollingSession);
    server.use(
      http.post(apiPath('auth/totp/enrollment'), () =>
        apiError(500, 'internal', 'something went wrong on the server'),
      ),
    );
    renderApp('/login/enroll');
    await userEvent
      .setup()
      .click(await screen.findByRole('button', { name: 'Use an authenticator app' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Something went wrong on the server.',
    );
  });

  it('signs out from enrollment', async () => {
    signedInAs(enrollingSession);
    server.use(http.post(apiPath('auth/logout'), () => new Response(null, { status: 204 })));
    renderApp('/login/enroll');
    await userEvent
      .setup()
      .click(await screen.findByRole('button', { name: 'Cancel and sign out' }));
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeVisible();
  });
});
