// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import { adminSession, viewerSession } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, server, signedInAs } from '../../test/server';

describe('app shell', () => {
  it('shows the audit log link only to admins', async () => {
    signedInAs(viewerSession);
    renderApp('/');
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    expect(within(nav).getByRole('link', { name: 'Account' })).toBeVisible();
    expect(within(nav).queryByRole('link', { name: 'Audit log' })).not.toBeInTheDocument();
  });

  it('marks the current page in the navigation', async () => {
    signedInAs(adminSession);
    renderApp('/');
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    expect(within(nav).getByRole('link', { name: 'Machines' })).toHaveAttribute(
      'aria-current',
      'page',
    );
  });

  it('signs out', async () => {
    signedInAs(adminSession);
    server.use(http.post(apiPath('auth/logout'), () => new Response(null, { status: 204 })));
    renderApp('/');
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeVisible();
  });

  it('answers an unknown address with a not-found page', async () => {
    signedInAs(adminSession);
    renderApp('/no/such/page');
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeVisible();
  });

  it('shows a retry screen when the server can’t be reached', async () => {
    server.use(
      http.get(apiPath('setup/status'), () => apiError(502, 'unknown', 'bad gateway')),
      http.get(apiPath('auth/session'), () => apiError(502, 'unknown', 'bad gateway')),
    );
    renderApp('/');
    expect(await screen.findByRole('heading', { name: 'Sentinel can’t be reached' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeVisible();
  });
});
