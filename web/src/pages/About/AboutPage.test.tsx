// SPDX-License-Identifier: Apache-2.0
import { screen, within } from '@testing-library/react';
import { http } from 'msw';

import type { AboutResponse } from '../../api/types.gen';
import { findAccessibilityViolations } from '../../test/axe';
import { viewerSession } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiPath, json, server, signedInAs } from '../../test/server';

function withAbout(about: AboutResponse) {
  signedInAs(viewerSession);
  server.use(http.get(apiPath('about'), () => json(about)));
}

describe('§9.11 about page', () => {
  it('shows the server version, database size and agent versions, most common first', async () => {
    withAbout({
      serverVersion: 'v0.1.0-beta.1',
      databaseBytes: 52_428_800,
      machines: 13,
      agentVersions: [
        { version: 'v0.1.0-beta.1', machines: 12 },
        { version: 'v0.0.9', machines: 1 },
      ],
    });
    const { container } = renderApp('/about');
    expect(await screen.findByRole('heading', { name: 'About Sentinel' })).toBeVisible();
    expect(await screen.findByText('v0.1.0-beta.1', { selector: 'dd' })).toBeVisible();
    expect(screen.getByText('50 MB')).toBeVisible();
    expect(screen.getByText('13 machines')).toBeVisible();
    const versions = within(screen.getByRole('region', { name: 'Agent versions' })).getAllByRole(
      'listitem',
    );
    expect(versions.map((item) => item.textContent)).toEqual([
      'v0.1.0-beta.112 machines',
      'v0.0.91 machine',
    ]);
    expect(await findAccessibilityViolations(container)).toEqual([]);
  });

  it('says when there are no machines yet', async () => {
    withAbout({ serverVersion: 'dev', databaseBytes: 4096, machines: 0, agentVersions: [] });
    renderApp('/about');
    expect(await screen.findByText('No machines yet.')).toBeVisible();
    expect(screen.getByRole('link', { name: 'About' })).toHaveAttribute('aria-current', 'page');
  });
});
