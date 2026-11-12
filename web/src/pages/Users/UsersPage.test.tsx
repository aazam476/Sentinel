// SPDX-License-Identifier: Apache-2.0
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type { UserResponse } from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { adminSession, fixtureNow, viewerSession } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

const people: UserResponse[] = [
  {
    id: 1,
    username: 'admin',
    role: 'admin',
    createdAt: fixtureNow,
    disabled: false,
    twoFactor: true,
  },
  {
    id: 3,
    username: 'olu',
    role: 'operator',
    createdAt: fixtureNow,
    disabled: false,
    twoFactor: false,
  },
  {
    id: 4,
    username: 'gone',
    role: 'viewer',
    createdAt: fixtureNow,
    disabled: true,
    twoFactor: false,
  },
];

function withUsers() {
  signedInAs(adminSession);
  const sent: string[] = [];
  let users = people;
  server.use(
    http.get(apiPath('users'), () => json({ users })),
    http.patch(apiPath('users/:id'), async ({ params, request }) => {
      const body = await request.text();
      sent.push(`${String(params.id)} ${body}`);
      const change = JSON.parse(body) as Partial<UserResponse>;
      users = users.map((user) => (String(user.id) === params.id ? { ...user, ...change } : user));
      return json(users.find((user) => String(user.id) === params.id) ?? {});
    }),
  );
  return sent;
}

describe('Users', () => {
  it('lists users, marking the signed-in one and the disabled ones', async () => {
    withUsers();
    const { container } = renderApp('/users');
    const me = await screen.findByRole('listitem', { name: 'admin' });
    expect(within(me).getByText('You')).toBeVisible();
    expect(within(me).getByRole('combobox', { name: 'Role' })).toBeDisabled();
    expect(within(me).queryByRole('button', { name: 'Disable' })).toBeNull();
    const gone = screen.getByRole('listitem', { name: 'gone' });
    expect(within(gone).getByText('Disabled')).toBeVisible();
    expect(within(gone).getByRole('button', { name: 'Enable' })).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('changes a role, and disables someone after confirmation', async () => {
    const sent = withUsers();
    const user = userEvent.setup();
    renderApp('/users');
    const olu = await screen.findByRole('listitem', { name: 'olu' });
    await user.selectOptions(within(olu).getByRole('combobox', { name: 'Role' }), 'admin');
    expect(await screen.findByText('olu is now admin')).toBeVisible();
    await user.click(within(olu).getByRole('button', { name: 'Disable' }));
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Disable' }),
    );
    expect(await within(olu).findByText('Disabled')).toBeVisible();
    expect(sent).toEqual(['3 {"role":"admin"}', '3 {"disabled":true}']);
  });

  it('adds a user, asking for a fresh second factor first when the server wants one', async () => {
    withUsers();
    const user = userEvent.setup();
    const created: unknown[] = [];
    let attempts = 0;
    server.use(
      http.post(apiPath('users'), async ({ request }) => {
        attempts++;
        if (attempts === 1) {
          return apiError(403, 'step_up_required', 'confirm it’s you');
        }
        created.push(await request.json());
        return json(
          { id: 5, username: 'nia', role: 'operator', createdAt: fixtureNow, disabled: false },
          201,
        );
      }),
      http.post(apiPath('auth/step-up'), () => json(adminSession)),
    );
    renderApp('/users');
    await user.click(await screen.findByRole('button', { name: 'Add user' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add a user' });
    await user.type(within(dialog).getByRole('textbox', { name: 'Username' }), ' nia ');
    await user.type(within(dialog).getByLabelText('First password'), 'a long first password');
    await user.selectOptions(within(dialog).getByRole('combobox', { name: 'Role' }), 'operator');
    expect(within(dialog).getByText(/Also uses terminals/)).toBeVisible();
    await user.click(within(dialog).getByRole('button', { name: 'Add user' }));

    const stepUp = await screen.findByRole('dialog', { name: /Confirm/ });
    await user.type(within(stepUp).getByRole('textbox'), '123456');
    await user.click(within(stepUp).getByRole('button', { name: /Confirm/ }));
    await waitFor(() => {
      expect(created).toEqual([
        { username: 'nia', password: 'a long first password', role: 'operator' },
      ]);
    });
    expect(await screen.findByText('Added nia')).toBeVisible();
  }, 20_000);

  it('shows the server’s objection in the dialog', async () => {
    withUsers();
    const user = userEvent.setup();
    server.use(
      http.post(apiPath('users'), () => apiError(409, 'username_taken', 'that username is taken')),
    );
    renderApp('/users');
    await user.click(await screen.findByRole('button', { name: 'Add user' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add a user' });
    await user.type(within(dialog).getByRole('textbox', { name: 'Username' }), 'olu');
    await user.type(within(dialog).getByLabelText('First password'), 'a long first password');
    await user.click(within(dialog).getByRole('button', { name: 'Add user' }));
    expect(await within(dialog).findByText('That username is taken.')).toBeVisible();
  });

  it('tells non-admins they can’t, and keeps the link out of their menu', async () => {
    signedInAs(viewerSession);
    renderApp('/users');
    expect(await screen.findByText('Only admins manage users.')).toBeVisible();
    expect(screen.queryByRole('link', { name: 'Users' })).toBeNull();
  });

  it('SEC-04 marks users without 2FA and requires it for everyone after confirming', async () => {
    withUsers();
    let saved: { requireTwoFactor: boolean } | undefined;
    server.use(
      http.put(apiPath('settings/security'), async ({ request }) => {
        saved = (await request.json()) as { requireTwoFactor: boolean };
        return json(saved);
      }),
    );
    const user = userEvent.setup();
    renderApp('/users');
    const olu = await screen.findByRole('listitem', { name: 'olu' });
    expect(within(olu).getByText('No 2FA')).toBeVisible();
    expect(
      within(screen.getByRole('listitem', { name: 'admin' })).queryByText('No 2FA'),
    ).toBeNull();

    const toggle = await screen.findByRole('checkbox', { name: 'Require it for everyone' });
    expect(toggle).not.toBeChecked();
    await user.click(toggle);
    const dialog = await screen.findByRole('alertdialog');
    // Disabled users can't sign in anyway, so only olu is counted.
    expect(within(dialog).getByText(/1 user without it will be signed out/)).toBeVisible();
    await user.click(within(dialog).getByRole('button', { name: 'Require it' }));
    expect(await screen.findByText('Two-factor authentication is now required')).toBeVisible();
    expect(saved).toEqual({ requireTwoFactor: true });
    expect(await screen.findByRole('checkbox', { name: 'Require it for everyone' })).toBeChecked();
  });

  it('explains why an admin without 2FA can’t require it', async () => {
    withUsers();
    server.use(
      http.put(apiPath('settings/security'), () =>
        apiError(
          409,
          'own_second_factor_needed',
          'set up two-factor authentication for your own account first',
        ),
      ),
    );
    const user = userEvent.setup();
    renderApp('/users');
    await user.click(await screen.findByRole('checkbox', { name: 'Require it for everyone' }));
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Require it' }),
    );
    expect(
      await screen.findByText(/Set up two-factor authentication for your own account first/i),
    ).toBeVisible();
  });
});
