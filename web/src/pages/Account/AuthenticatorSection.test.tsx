// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type { SessionResponse, SignedInResponse } from '../../api/types.gen';
import { adminSession, fixtureNow, sessions } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const withoutTwoFactor: SessionResponse = {
  ...adminSession,
  secondFactors: { totp: false, passkey: false },
};
const codes = Array.from({ length: 10 }, (_, i) => `AAAA-BBBB-CCCC-000${String(i)}`);

function mockAccount(session: SessionResponse, requireTwoFactor = false) {
  signedInAs(session);
  server.use(
    http.get(apiPath('auth/passkeys'), () => json({ passkeys: [] })),
    http.get(apiPath('auth/sessions'), () => json({ sessions })),
    http.get(apiPath('settings/security'), () => json({ requireTwoFactor })),
  );
}

describe('ADR 0039 authenticator app on the account page', () => {
  it('sets one up, shows the recovery codes, then counts it as set up', async () => {
    mockAccount(withoutTwoFactor);
    const withTOTP: SessionResponse = { ...adminSession };
    server.use(
      http.post(apiPath('auth/totp/enrollment'), () =>
        json({
          secret: 'JBSWY3DPEHPK3PXP',
          url: 'otpauth://totp/x',
          qrCode: 'data:image/png;base64,iVBORw0KGgo=',
        }),
      ),
      http.post(apiPath('auth/totp/enrollment/confirm'), () => {
        const response: SignedInResponse = {
          session: withTOTP,
          recoveryCodes: codes,
          remainingRecoveryCodes: null,
        };
        return json(response);
      }),
    );
    const user = userEvent.setup();
    renderApp('/account');
    await user.click(await screen.findByRole('button', { name: 'Set up authenticator app' }));
    await user.type(await screen.findByLabelText('Authenticator code'), '123456');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));
    const list = await screen.findByRole('list', { name: 'Recovery codes' });
    expect(list.children).toHaveLength(10);
    await user.click(screen.getByLabelText('I have saved these codes'));
    await user.click(screen.getByRole('button', { name: 'Continue' }));
    expect(await screen.findByText('Authenticator app added')).toBeVisible();
    expect(await screen.findByRole('button', { name: 'Remove authenticator app' })).toBeVisible();
  });

  it('removes it after confirming, asking for the password first when the check is old', async () => {
    mockAccount({ ...adminSession, stepUpValidUntil: fixtureNow - 1 });
    let stepUps = 0;
    let removed = false;
    server.use(
      http.delete(apiPath('auth/totp'), () => {
        if (stepUps === 0) {
          return apiError(403, 'step_up_required', 'confirm it’s you first');
        }
        removed = true;
        return new Response(null, { status: 204 });
      }),
      http.post(apiPath('auth/step-up'), async ({ request }) => {
        stepUps++;
        expect(await request.json()).toEqual({ totpCode: '123456' });
        return json(adminSession);
      }),
    );
    const user = userEvent.setup();
    renderApp('/account');
    await user.click(await screen.findByRole('button', { name: 'Remove authenticator app' }));
    const confirm = await screen.findByRole('alertdialog');
    expect(within(confirm).getByText(/recovery codes stop working/)).toBeVisible();
    await user.click(within(confirm).getByRole('button', { name: 'Remove' }));
    const stepUp = await screen.findByRole('dialog', { name: 'Confirm it’s you' });
    await user.type(within(stepUp).getByLabelText('Authenticator code'), '123456');
    await user.click(within(stepUp).getByRole('button', { name: 'Confirm' }));
    expect(await screen.findByText('Authenticator app removed')).toBeVisible();
    expect(removed).toBe(true);
  });

  it('won’t offer to remove the last second factor where 2FA is required', async () => {
    mockAccount(adminSession, true);
    renderApp('/account');
    expect(await screen.findByText(/This server requires two-factor authentication/)).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Remove authenticator app' })).toBeNull();
  });

  it('SEC-07 asks a user without 2FA for their password to confirm', async () => {
    mockAccount({ ...withoutTwoFactor, stepUpValidUntil: fixtureNow - 1 });
    let sent: unknown;
    server.use(
      http.post(apiPath('auth/totp/enrollment'), () =>
        apiError(403, 'step_up_required', 'confirm it’s you first'),
      ),
      http.post(apiPath('auth/step-up'), async ({ request }) => {
        sent = await request.json();
        return apiError(401, 'invalid_credentials', 'the username or password is incorrect');
      }),
    );
    const user = userEvent.setup();
    renderApp('/account');
    await user.click(await screen.findByRole('button', { name: 'Set up authenticator app' }));
    const stepUp = await screen.findByRole('dialog', { name: 'Confirm it’s you' });
    expect(within(stepUp).getByText('This change needs your password again.')).toBeVisible();
    await user.type(within(stepUp).getByLabelText('Password'), 'my password');
    await user.click(within(stepUp).getByRole('button', { name: 'Confirm' }));
    expect(await within(stepUp).findByText('The username or password is incorrect.')).toBeVisible();
    expect(sent).toEqual({ password: 'my password' });
  });
});
