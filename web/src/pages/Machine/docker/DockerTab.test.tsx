// SPDX-License-Identifier: Apache-2.0
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { vi } from 'vitest';

import type { ContainerListPayload, StackListPayload } from '../../../api/protocol.gen';
import type { DockerImages } from '../../../api/types.gen';
import { findAccessibilityViolations } from '../../../test/axe';
import { FakeEventSource, useFakeEventSource } from '../../../test/fakeEventSource';
import { adminSession, nasDetails, viewerSession } from '../../../test/fixtures/normal/fixtures';
import { renderApp } from '../../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../../test/server';

// CodeMirror needs a real browser's layout; the end-to-end test drives it.
// Here a text area stands in, with the same label and value.
vi.mock('../../../components/CodeEditor/CodeEditor', () => ({
  CodeEditor: ({
    label,
    value,
    onChange,
  }: {
    label: string;
    value: string;
    onChange: (v: string) => void;
  }) => (
    <textarea
      aria-label={label}
      value={value}
      onChange={(event) => {
        onChange(event.target.value);
      }}
    />
  ),
}));

const base = `/machines/${nasDetails.id}/docker`;
const docker = (path: string) => apiPath(`agents/${nasDetails.id}/docker${path}`);

const containers: ContainerListPayload = {
  engine: { name: 'Docker', version: '29.9.2', socket: 'unix:///var/run/docker.sock' },
  containers: [
    {
      id: 'a1',
      name: 'blog-web-1',
      image: 'nginx:1.27',
      state: 'running',
      status: 'Up 3 hours',
      created: 0,
      ports: [{ ip: '0.0.0.0', privatePort: 80, publicPort: 8080, protocol: 'tcp' }],
      stack: 'blog',
      service: 'web',
      cpuPercent: 2.5,
      memoryBytes: 52428800,
      memoryLimitBytes: 0,
    },
    {
      id: 'b2',
      name: 'old-job',
      image: 'alpine:3.20',
      state: 'exited',
      status: 'Exited (0) 2 days ago',
      created: 0,
      ports: [],
      cpuPercent: 0,
      memoryBytes: 0,
      memoryLimitBytes: 0,
    },
  ],
};

/** recordChanges records every non-GET request to the machine's Docker API. */
function recordChanges() {
  const changes: string[] = [];
  const record = async ({ request }: { request: Request }) => {
    const url = new URL(request.url);
    const body = await request.text();
    changes.push(
      `${request.method} ${url.pathname.replace(`/api/agents/${nasDetails.id}/docker`, '')}${url.search}${body === '' ? '' : ` ${body}`}`,
    );
    return new HttpResponse(null, { status: 204 });
  };
  server.use(
    http.post(docker('/*'), record),
    http.put(docker('/*'), record),
    http.delete(docker('/*'), record),
  );
  return changes;
}

function withMachine(session = adminSession) {
  signedInAs(session);
  server.use(http.get(apiPath(`agents/${nasDetails.id}`), () => json(nasDetails)));
}

describe('Docker containers', () => {
  it('lists containers and controls them, asking before removing a running one', async () => {
    withMachine();
    server.use(http.get(docker('/containers'), () => json(containers)));
    const changes = recordChanges();
    const user = userEvent.setup();
    const { container } = renderApp(base);

    expect(await screen.findByText('2 containers · Docker 29.9.2')).toBeVisible();
    const web = screen.getByRole('article', { name: 'blog-web-1' });
    expect(within(web).getByText('CPU 2.5%')).toBeVisible();
    expect(within(web).getByText('8080→80/tcp')).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.click(within(web).getByRole('button', { name: 'Actions for blog-web-1' }));
    expect((await screen.findAllByRole('menuitem')).map((item) => item.textContent)).toEqual([
      'Logs',
      'Shell',
      'Restart',
      'Stop',
      'Remove',
    ]);
    await user.click(screen.getByRole('menuitem', { name: 'Restart' }));
    expect(await screen.findByText('Restart: blog-web-1')).toBeVisible();

    await user.click(within(web).getByRole('button', { name: 'Actions for blog-web-1' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Remove' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Remove blog-web-1?' });
    expect(dialog).toHaveTextContent('It is running');
    await user.click(within(dialog).getByRole('button', { name: 'Remove' }));
    await waitFor(() => {
      expect(changes).toEqual([
        'POST /containers/blog-web-1/restart',
        'DELETE /containers/blog-web-1?force=true',
      ]);
    });
  });

  it('offers viewers only the logs', async () => {
    withMachine(viewerSession);
    server.use(http.get(docker('/containers'), () => json(containers)));
    const user = userEvent.setup();
    renderApp(base);
    await user.click(await screen.findByRole('button', { name: 'Actions for blog-web-1' }));
    expect((await screen.findAllByRole('menuitem')).map((item) => item.textContent)).toEqual([
      'Logs',
    ]);
  });

  it('says when the machine has no Docker', async () => {
    withMachine();
    server.use(
      http.get(docker('/containers'), () =>
        apiError(503, 'docker_unavailable', 'docker not detected'),
      ),
    );
    renderApp(base);
    expect(await screen.findByText(/^Docker not detected\./)).toBeVisible();
  });

  it('follows a container’s log live, and says why it stopped', async () => {
    useFakeEventSource();
    withMachine();
    server.use(http.get(docker('/containers'), () => json(containers)));
    const { router } = renderApp(`${base}/containers/blog-web-1/logs`);
    await waitFor(() => {
      expect(FakeEventSource.latest?.url).toContain('/docker/containers/blog-web-1/logs/follow');
    });
    const source = FakeEventSource.latest as FakeEventSource;
    source.dispatchEvent(new Event('open'));
    source.dispatchEvent(
      new MessageEvent('line', {
        data: JSON.stringify({ time: 1, stream: 'stdout', text: 'GET / 200' }),
      }),
    );
    source.dispatchEvent(
      new MessageEvent('line', {
        data: JSON.stringify({ time: 2, stream: 'stderr', text: 'warn: slow' }),
      }),
    );
    expect(await screen.findByText('warn: slow')).toBeVisible();
    expect(screen.getByText('Following · 2 lines')).toBeVisible();
    await userEvent.type(screen.getByRole('searchbox', { name: 'Filter lines' }), 'GET');
    await waitFor(() => {
      expect(screen.queryByText('warn: slow')).not.toBeInTheDocument();
    });
    source.dispatchEvent(
      new MessageEvent('end', { data: JSON.stringify({ reason: 'the container stopped' }) }),
    );
    expect(await screen.findByText('Stopped following: the container stopped')).toBeVisible();
    expect(source.closed).toBe(true);

    await userEvent.click(screen.getByRole('link', { name: '← Containers' }));
    expect(router.state.location.pathname).toBe(base);
  });
});

describe('Docker images', () => {
  const images: DockerImages = {
    images: [
      {
        id: 'sha256:aaa',
        tags: ['nginx:1.27'],
        digests: [],
        sizeBytes: 190_000_000,
        created: 0,
        containers: 1,
      },
      { id: 'sha256:bbb', tags: [], digests: [], sizeBytes: 5_000_000, created: 0, containers: 0 },
    ],
    updates: { 'nginx:1.27': { updateAvailable: true, error: '', checkedAt: Date.now() - 60_000 } },
  };

  it('shows updates, pulls, checks, and lets admins add registry logins', async () => {
    withMachine();
    server.use(
      http.get(docker('/images'), () => json(images)),
      http.get(apiPath('registries'), () => json({ logins: [] })),
      http.post(apiPath('registries'), () =>
        json({ id: 1, serverAddress: 'ghcr.io', username: 'bot', createdAt: 0 }, 201),
      ),
    );
    const changes = recordChanges();
    const user = userEvent.setup();
    const { container } = renderApp(`${base}/images`);

    expect(await screen.findByText('2 images · 1 update available')).toBeVisible();
    expect(
      within(screen.getByRole('article', { name: 'nginx:1.27' })).getByText('Update available'),
    ).toBeVisible();
    expect(screen.getByRole('article', { name: 'bbb' })).toBeVisible(); // an untagged image, by short ID
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.type(screen.getByRole('textbox', { name: 'Image to pull' }), 'redis:7');
    await user.click(screen.getByRole('button', { name: 'Pull' }));
    expect(await screen.findByText('Pulled redis:7')).toBeVisible();

    await user.type(screen.getByRole('textbox', { name: 'Registry' }), 'ghcr.io');
    await user.type(screen.getByRole('textbox', { name: 'User name' }), 'bot');
    await user.type(screen.getByLabelText('Password or token'), 'ghp_token');
    await user.click(screen.getByRole('button', { name: 'Add login' }));
    expect(await screen.findByText('Saved the login for ghcr.io')).toBeVisible();
    expect(changes).toEqual(['POST /images/pull {"reference":"redis:7"}']);
  });

  it('hides registry logins and actions from viewers', async () => {
    withMachine(viewerSession);
    server.use(http.get(docker('/images'), () => json(images)));
    renderApp(`${base}/images`);
    expect(await screen.findByText('2 images · 1 update available')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Pull' })).not.toBeInTheDocument();
    expect(screen.queryByText('Registry logins')).not.toBeInTheDocument();
  });
});

describe('Docker volumes, networks and stacks', () => {
  it('removes only what nothing uses', async () => {
    withMachine();
    server.use(
      http.get(docker('/volumes'), () =>
        json({
          volumes: [
            {
              name: 'db-data',
              driver: 'local',
              mountpoint: '/var/lib/docker/volumes/db-data/_data',
              created: 0,
              inUse: true,
            },
            {
              name: 'old-data',
              driver: 'local',
              mountpoint: '/var/lib/docker/volumes/old-data/_data',
              created: 0,
              inUse: false,
            },
          ],
        }),
      ),
      http.get(docker('/networks'), () =>
        json({
          networks: [
            {
              id: '1',
              name: 'bridge',
              driver: 'bridge',
              scope: 'local',
              containers: 0,
              builtIn: true,
            },
            {
              id: '2',
              name: 'old-net',
              driver: 'bridge',
              scope: 'local',
              containers: 0,
              builtIn: false,
            },
          ],
        }),
      ),
    );
    const changes = recordChanges();
    const user = userEvent.setup();
    renderApp(`${base}/volumes`);
    expect(await screen.findByText('2 volumes')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Remove volume db-data' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove network bridge' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Remove volume old-data' }));
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Remove' }),
    );
    await waitFor(() => {
      expect(changes).toEqual(['DELETE /volumes/old-data']);
    });
  });

  it('creates a stack from a pasted compose file, checks it and deploys it', async () => {
    withMachine();
    const stacks: StackListPayload = {
      dir: '/opt/sentinel/stacks',
      stacks: [
        {
          name: 'media',
          managed: false,
          containers: { running: 2, exited: 1 },
          configFiles: ['/srv/media/compose.yml'],
          editable: false,
        },
      ],
    };
    server.use(
      http.get(docker('/stacks'), () => json(stacks)),
      http.get(docker('/stacks/shop'), () =>
        json({
          name: 'shop',
          managed: true,
          editable: true,
          compose: 'services: {}\n',
          env: '',
          services: [],
        }),
      ),
    );
    const changes = recordChanges();
    const user = userEvent.setup();
    const { router } = renderApp(`${base}/stacks`);

    expect(await screen.findByText('1 stack · kept in /opt/sentinel/stacks')).toBeVisible();
    expect(screen.getByText('2 of 3 containers running')).toBeVisible();
    await user.click(screen.getByRole('link', { name: 'New stack' }));
    await user.type(await screen.findByRole('textbox', { name: 'Stack name' }), 'Shop');
    await user.click(screen.getByRole('textbox', { name: 'compose.yaml' }));
    await user.paste('services: {}\n');
    await user.click(screen.getByRole('button', { name: 'Validate' }));
    expect(await screen.findByText('The compose file is valid')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Save and deploy' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`${base}/stacks/shop`);
    });
    expect(changes).toEqual([
      'PUT /stacks/shop {"compose":"services: {}\\n","env":"","validateOnly":true}',
      'PUT /stacks/shop {"compose":"services: {}\\n","env":"","validateOnly":false}',
      'POST /stacks/shop/up',
    ]);
  });
});
