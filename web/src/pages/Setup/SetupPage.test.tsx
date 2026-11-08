// SPDX-License-Identifier: Apache-2.0
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type {
  BeginSetupRequest,
  BeginSetupResponse,
  CompleteSetupRequest,
  SignedInResponse,
} from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { adminSession } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const codes = Array.from({ length: 10 }, (_, i) => `AAAA-BBBB-CCCC-000${String(i)}`);

function mockSetup() {
  const begun: BeginSetupResponse = {
    setupId: 'setup-1',
    totp: {
      secret: 'JBSWY3DPEHPK3PXP',
      url: 'otpauth://totp/Sentinel:admin',
      qrCode: 'data:image/png;base64,iVBORw0KGgo=',
    },
  };
  const signedIn: SignedInResponse = {
    session: adminSession,
    recoveryCodes: codes,
    remainingRecoveryCodes: null,
  };
  const requests: { begin?: BeginSetupRequest; complete?: CompleteSetupRequest } = {};
  server.use(
    http.post(apiPath('setup/begin'), async ({ request }) => {
      const body = (await request.json()) as BeginSetupRequest;
      requests.begin = body;
      if (body.setupToken !== 'GOOD-TOKEN') {
        return apiError(403, 'invalid_setup_token', 'the setup token is incorrect');
      }
      if (body.password.length < 12) {
        return apiError(400, 'invalid_input', 'the password must be at least 12 characters', {
          field: 'password',
        });
      }
      return json(begun);
    }),
    http.post(apiPath('setup/complete'), async ({ request }) => {
      const body = (await request.json()) as CompleteSetupRequest;
      requests.complete = body;
      return body.totpCode === '123456'
        ? json(signedIn)
        : apiError(401, 'invalid_code', 'the code is incorrect or has already been used');
    }),
  );
  return requests;
}

async function fillDetails(token: string, password: string, confirmation = password) {
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText('Setup token'), token);
  await user.type(screen.getByLabelText('Password'), password);
  await user.type(screen.getByLabelText('Confirm password'), confirmation);
  await user.click(screen.getByRole('button', { name: 'Continue' }));
  return user;
}

describe('SEC-01 setup page', () => {
  it('sends a fresh install to setup from any page', async () => {
    signedInAs(null, true);
    renderApp('/account');
    expect(await screen.findByRole('heading', { name: 'Set up Sentinel' })).toBeVisible();
  });

  it('can’t be reached once setup is done', async () => {
    signedInAs(null, false);
    renderApp('/setup');
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeVisible();
  });

  it('explains a wrong setup token at the token field', async () => {
    signedInAs(null, true);
    mockSetup();
    renderApp('/setup');
    await fillDetails('WRONG', 'correct horse battery staple');
    expect(await screen.findByText('This setup token is incorrect.')).toBeVisible();
    expect(screen.getByLabelText('Setup token')).toHaveAttribute('aria-invalid', 'true');
  });

  it('SEC-02 shows the server’s password rule at the password field', async () => {
    signedInAs(null, true);
    mockSetup();
    renderApp('/setup');
    await fillDetails('GOOD-TOKEN', 'short');
    expect(await screen.findByText('the password must be at least 12 characters')).toBeVisible();
  });

  it('catches mismatched passwords before asking the server', async () => {
    signedInAs(null, true);
    const requests = mockSetup();
    renderApp('/setup');
    await fillDetails('GOOD-TOKEN', 'correct horse battery staple', 'something else entirely');
    expect(await screen.findByText('The passwords don’t match.')).toBeVisible();
    expect(requests.begin).toBeUndefined();
  });

  it('SEC-02 walks through the token, TOTP and recovery codes into the app', async () => {
    signedInAs(null, true);
    const requests = mockSetup();
    renderApp('/setup');

    const user = await fillDetails('  GOOD-TOKEN ', 'correct horse battery staple');
    expect(requests.begin).toEqual({
      setupToken: 'GOOD-TOKEN',
      username: 'admin',
      password: 'correct horse battery staple',
    });
    expect(
      await screen.findByRole('img', { name: 'QR code for your authenticator app' }),
    ).toBeVisible();
    expect(screen.getByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument();

    await user.type(screen.getByLabelText('Authenticator code'), '000000');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The code is incorrect or has already been used.',
    );

    await user.clear(screen.getByLabelText('Authenticator code'));
    await user.type(screen.getByLabelText('Authenticator code'), '123456');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(requests.complete).toEqual({
      setupToken: 'GOOD-TOKEN',
      setupId: 'setup-1',
      totpCode: '123456',
    });

    const list = await screen.findByRole('list', { name: 'Recovery codes' });
    expect(list.children).toHaveLength(10);
    const continueButton = screen.getByRole('button', { name: 'Continue' });
    expect(continueButton).toBeDisabled();
    await user.click(screen.getByLabelText('I have saved these codes'));
    await user.click(continueButton);
    expect(await screen.findByRole('heading', { name: 'Machines' })).toBeVisible();
  });

  it('has no accessibility violations', async () => {
    signedInAs(null, true);
    const { container } = renderApp('/setup');
    await screen.findByRole('heading', { name: 'Set up Sentinel' });
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });
});
