// SPDX-License-Identifier: Apache-2.0
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type {
  LoginRequest,
  SecondFactorRequest,
  SessionResponse,
  SignedInResponse,
} from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { adminSession, pendingSession } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server } from '../../test/server';

/**
 * mockSignIn follows the real server: the session changes as each step
 * succeeds, and the pages move with it.
 */
function mockSignIn() {
  let session: SessionResponse | null = null;
  const secondFactorRequests: SecondFactorRequest[] = [];
  server.use(
    http.get(apiPath('setup/status'), () => json({ setupRequired: false })),
    http.get(apiPath('auth/session'), () =>
      session === null ? apiError(401, 'not_authenticated', 'not signed in') : json(session),
    ),
    http.post(apiPath('auth/login'), async ({ request }) => {
      const body = (await request.json()) as LoginRequest;
      if (body.password !== 'correct horse battery staple') {
        return apiError(401, 'invalid_credentials', 'the username or password is incorrect');
      }
      session = pendingSession;
      return json({ nextStep: 'second_factor' });
    }),
    http.post(apiPath('auth/login/second-factor'), async ({ request }) => {
      const body = (await request.json()) as SecondFactorRequest;
      secondFactorRequests.push(body);
      if (body.totpCode === '123456' || body.recoveryCode === 'AAAA-BBBB-CCCC-DDDD') {
        session = adminSession;
        const response: SignedInResponse = {
          session: adminSession,
          recoveryCodes: [],
          remainingRecoveryCodes: body.recoveryCode === undefined ? null : 1,
        };
        return json(response);
      }
      return apiError(429, 'too_many_attempts', 'too many failed attempts', {
        retryAfterSeconds: 4,
      });
    }),
    http.post(apiPath('auth/logout'), () => {
      session = null;
      return new Response(null, { status: 204 });
    }),
  );
  return secondFactorRequests;
}

async function enterPassword(password: string) {
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText('Username'), 'admin');
  await user.type(screen.getByLabelText('Password'), password);
  await user.click(screen.getByRole('button', { name: 'Sign in' }));
  return user;
}

describe('SEC-04 sign-in', () => {
  it('sends a signed-out browser to sign-in from any page', async () => {
    mockSignIn();
    renderApp('/audit');
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeVisible();
  });

  it('shows a wrong password as an alert', async () => {
    mockSignIn();
    renderApp('/login');
    await enterPassword('wrong password');
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The username or password is incorrect.',
    );
  });

  it('needs a second factor after the password before the app opens', async () => {
    mockSignIn();
    renderApp('/login');
    const user = await enterPassword('correct horse battery staple');

    expect(await screen.findByRole('heading', { name: 'Confirm it’s you' })).toBeVisible();
    expect(screen.queryByRole('navigation', { name: 'Main' })).not.toBeInTheDocument();

    await user.type(screen.getByLabelText('Authenticator code'), '123456');
    await user.click(screen.getByRole('button', { name: 'Verify' }));
    expect(await screen.findByRole('heading', { name: 'Machines' })).toBeVisible();
  });

  it('says how long to wait when attempts are limited', async () => {
    mockSignIn();
    renderApp('/login');
    const user = await enterPassword('correct horse battery staple');
    await user.type(await screen.findByLabelText('Authenticator code'), '999999');
    await user.click(screen.getByRole('button', { name: 'Verify' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Try again in 4 seconds.');
  });

  it('accepts a recovery code and warns how many are left', async () => {
    const requests = mockSignIn();
    renderApp('/login');
    const user = await enterPassword('correct horse battery staple');
    await user.click(await screen.findByRole('button', { name: 'Use a recovery code' }));
    await user.type(screen.getByLabelText('Recovery code'), 'AAAA-BBBB-CCCC-DDDD');
    await user.click(screen.getByRole('button', { name: 'Verify' }));

    expect(await screen.findByText('1 recovery code left')).toBeVisible();
    expect(requests).toEqual([{ recoveryCode: 'AAAA-BBBB-CCCC-DDDD' }]);
  });

  it('switches back from a recovery code to the authenticator', async () => {
    mockSignIn();
    renderApp('/login');
    const user = await enterPassword('correct horse battery staple');
    await user.click(await screen.findByRole('button', { name: 'Use a recovery code' }));
    await user.click(screen.getByRole('button', { name: 'Use your authenticator instead' }));
    expect(screen.getByLabelText('Authenticator code')).toBeVisible();
  });

  it('offers a passkey when the user has one', async () => {
    mockSignIn();
    renderApp('/login');
    await enterPassword('correct horse battery staple');
    // jsdom has no WebAuthn, so the passkey button is hidden like in an old browser.
    expect(await screen.findByLabelText('Authenticator code')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Use a passkey' })).not.toBeInTheDocument();
  });

  it('cancelling the second factor signs out', async () => {
    mockSignIn();
    renderApp('/login');
    const user = await enterPassword('correct horse battery staple');
    await user.click(await screen.findByRole('button', { name: 'Cancel' }));
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeVisible();
  });

  it('has no accessibility violations', async () => {
    mockSignIn();
    const { container } = renderApp('/login');
    await screen.findByRole('heading', { name: 'Sign in' });
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });
});
