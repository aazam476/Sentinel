// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import { http } from 'msw';

import type { AuditEntriesResponse } from '../api/types.gen';
import { findAccessibilityViolations } from './axe';
import {
  longUsername,
  worstAuditEntries,
  worstPasskeys,
  worstSession,
  worstSessions,
} from './fixtures/worst-case/fixtures';
import { renderApp } from './renderApp';
import { apiPath, json, server, signedInAs } from './server';

/**
 * These render every data-showing screen with the worst-case fixtures:
 * longest names, unbreakable strings, right-to-left text, emoji, empty
 * values, and 1,200 audit entries. jsdom can't measure layout, so these
 * check that nothing crashes, nothing is lost, and anything cut short has a
 * tooltip; the Playwright suite checks the same data for overflow in a real
 * browser at phone and desktop widths.
 */
describe('worst-case data', () => {
  it('shell: a 64-character username is shown in full in a tooltip', async () => {
    signedInAs(worstSession);
    renderApp('/');
    const header = await screen.findByRole('banner');
    expect(within(header).getByTitle(longUsername)).toHaveTextContent(longUsername);
    expect(screen.getByRole('heading', { name: `Welcome, ${longUsername}` })).toBeVisible();
  });

  it('account: long, right-to-left and one-letter passkey names, odd sessions', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath('auth/passkeys'), () => json({ passkeys: worstPasskeys })),
      http.get(apiPath('auth/sessions'), () => json({ sessions: worstSessions })),
    );
    const { container } = renderApp('/account');

    for (const passkey of worstPasskeys) {
      expect(await screen.findByText(passkey.name)).toBeInTheDocument();
    }
    expect(screen.getByText(/2001:0db8:85a3:0000:0000:8a2e:0370:7334/)).toBeInTheDocument();
    expect(screen.getAllByText(/Unknown browser/)).toHaveLength(3);
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('account: no passkeys at all', async () => {
    signedInAs(worstSession);
    server.use(
      http.get(apiPath('auth/passkeys'), () => json({ passkeys: [] })),
      http.get(apiPath('auth/sessions'), () => json({ sessions: worstSessions.slice(0, 1) })),
    );
    renderApp('/account');
    expect(await screen.findByText('No passkeys yet.')).toBeVisible();
  });

  it('audit log: 1,200 entries render only what is on screen, extremes first', async () => {
    signedInAs(worstSession);
    const entries = worstAuditEntries();
    const page: AuditEntriesResponse = { entries, nextBefore: null };
    server.use(http.get(apiPath('audit'), () => json(page)));
    renderApp('/audit');

    const rows = await screen.findAllByRole('article');
    expect(rows.length).toBeGreaterThan(0);
    expect(rows.length).toBeLessThan(50);
    expect(rows[0]).toHaveTextContent("' OR '1'='1");
    expect(rows[1]).toHaveTextContent('ünïcødé-用户-👩‍💻');
    expect(rows[2]).toHaveTextContent('No user');
  });

  it('audit log: an empty log', async () => {
    signedInAs(worstSession);
    server.use(http.get(apiPath('audit'), () => json({ entries: [], nextBefore: null })));
    renderApp('/audit');
    expect(await screen.findByText('No entries yet.')).toBeVisible();
  });
});
