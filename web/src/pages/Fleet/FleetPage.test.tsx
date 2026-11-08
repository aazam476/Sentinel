// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import { findAccessibilityViolations } from '../../test/axe';
import { eventStream, useFakeEventSource } from '../../test/fakeEventSource';
import {
  adminSession,
  agents,
  fixtureNow,
  viewerSession,
} from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiPath, json, server, signedInAs } from '../../test/server';

function withAgents(list = agents) {
  server.use(http.get(apiPath('agents'), () => json({ agents: list })));
}

describe('fleet view', () => {
  it('lists every machine with its status and current readings', async () => {
    signedInAs(adminSession);
    withAgents();
    const { container } = renderApp('/');

    expect(await screen.findByText('3 machines · 2 online')).toBeVisible();
    const nas = screen.getByRole('link', { name: /Living room NAS/ });
    expect(within(nas).getByText('Online')).toBeInTheDocument();
    expect(within(nas).getByText('23%')).toBeVisible();
    expect(within(nas).getByText('92%')).toBeVisible();
    expect(within(nas).getByText('Up 3 d 4 h')).toBeVisible();
    expect(nas).toHaveAttribute('href', '/machines/01JBX3K5Q7Z8M4N6P8R0T2V4W6');

    const web = screen.getByRole('link', { name: /web-01/ });
    expect(within(web).getByLabelText('RAM: not reported')).toBeInTheDocument();

    const laptop = screen.getByRole('link', { name: /laptop/ });
    expect(within(laptop).getByText(/Offline · last seen/)).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('narrows the list by search and by tag', async () => {
    signedInAs(adminSession);
    withAgents();
    renderApp('/');
    const user = userEvent.setup();

    await user.type(await screen.findByRole('searchbox', { name: 'Search machines' }), 'WEB');
    expect(screen.getByRole('link', { name: /web-01/ })).toBeVisible();
    expect(screen.queryByRole('link', { name: /laptop/ })).not.toBeInTheDocument();

    await user.clear(screen.getByRole('searchbox'));
    await user.click(screen.getByRole('button', { name: 'storage' }));
    expect(screen.getByRole('button', { name: 'storage' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getAllByRole('link', { name: /nas|web|laptop/i })).toHaveLength(1);

    await user.type(screen.getByRole('searchbox'), 'nothing like this');
    expect(screen.getByText('No machines match.')).toBeVisible();
  });

  it('offers admins a way to add the first machine', async () => {
    signedInAs(adminSession);
    renderApp('/');
    expect(await screen.findByRole('heading', { name: 'No machines yet' })).toBeVisible();
    expect(screen.getByRole('link', { name: 'Add machine' })).toHaveAttribute(
      'href',
      '/machines/add',
    );
  });

  it('tells viewers an admin adds machines', async () => {
    signedInAs(viewerSession);
    renderApp('/');
    expect(await screen.findByText(/An admin can add machines/)).toBeVisible();
    expect(screen.queryByRole('link', { name: 'Add machine' })).not.toBeInTheDocument();
  });

  it('updates readings live from the event stream and refetches on status changes', async () => {
    useFakeEventSource();
    signedInAs(adminSession);
    withAgents();
    renderApp('/');
    const nas = await screen.findByRole('link', { name: /Living room NAS/ });

    eventStream().emit('metrics', {
      agentId: '01JBX3K5Q7Z8M4N6P8R0T2V4W6',
      current: { time: fixtureNow + 10_000, uptimeSeconds: 60, values: { 'cpu.percent': 88 } },
    });
    expect(await within(nas).findByText('88%')).toBeVisible();

    withAgents(agents.slice(1));
    eventStream().emit('agent', { agentId: '01JBX3K5Q7Z8M4N6P8R0T2V4W6', kind: 'offline' });
    expect(await screen.findByText('2 machines · 1 online')).toBeVisible();
    expect(eventStream().url).toMatch(/\/api\/events$/);
  });

  it('ignores an event stream message it can’t read', async () => {
    useFakeEventSource();
    signedInAs(adminSession);
    withAgents();
    renderApp('/');
    await screen.findByRole('link', { name: /Living room NAS/ });
    eventStream().dispatchEvent(new MessageEvent('metrics', { data: 'not json' }));
    eventStream().dispatchEvent(new MessageEvent('agent', { data: '{' }));
    expect(screen.getByText('23%')).toBeVisible();
  });
});
