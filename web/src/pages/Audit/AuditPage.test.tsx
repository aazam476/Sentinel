// SPDX-License-Identifier: Apache-2.0
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http } from 'msw';

import type { AuditEntriesResponse, AuditVerifyResponse } from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { adminSession, auditEntries, viewerSession } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiPath, json, server, signedInAs } from '../../test/server';

function mockAudit(
  verify: AuditVerifyResponse = { checked: 2, valid: true, brokenAtId: 0, problem: '' },
) {
  const page: AuditEntriesResponse = { entries: auditEntries, nextBefore: null };
  server.use(
    http.get(apiPath('audit'), () => json(page)),
    http.get(apiPath('audit/verify'), () => json(verify)),
  );
}

describe('SEC-41 audit log page', () => {
  it('shows entries newest first with their outcome, user and details', async () => {
    signedInAs(adminSession);
    mockAudit();
    renderApp('/audit');

    const rows = await screen.findAllByRole('article');
    expect(rows[0]).toHaveAccessibleName('Entry 2: auth.login');
    expect(rows[1]).toHaveTextContent('failure');
    expect(rows[1]).toHaveTextContent('mallory');
    expect(rows[1]).toHaveTextContent('reason: invalid_credentials');
  });

  it('offers CSV and JSON downloads', async () => {
    signedInAs(adminSession);
    mockAudit();
    renderApp('/audit');

    expect(await screen.findByRole('link', { name: 'Export CSV' })).toHaveAttribute(
      'href',
      expect.stringContaining('api/audit/export?format=csv'),
    );
    expect(screen.getByRole('link', { name: 'Export JSON' })).toHaveAttribute('download');
  });

  it('verifies the chain and says it is intact', async () => {
    signedInAs(adminSession);
    mockAudit();
    renderApp('/audit');
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Verify integrity' }));
    expect(await screen.findByRole('status')).toHaveTextContent('Intact: 2 entries check out.');
  });

  it('reports tampering', async () => {
    signedInAs(adminSession);
    mockAudit({
      checked: 7,
      valid: false,
      brokenAtId: 5,
      problem: 'entry 5 was changed after it was written',
    });
    renderApp('/audit');
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Verify integrity' }));
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Tampering detected at entry 5: entry 5 was changed after it was written',
    );
  });

  it('tells non-admins it is admin-only, without asking the server', async () => {
    signedInAs(viewerSession);
    renderApp('/audit');
    expect(await screen.findByText('Only admins can see the audit log.')).toBeVisible();
  });

  it('has no accessibility violations', async () => {
    signedInAs(adminSession);
    mockAudit();
    const { container } = renderApp('/audit');
    await screen.findAllByRole('article');
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });
});
