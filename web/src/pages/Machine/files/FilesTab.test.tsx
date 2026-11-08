// SPDX-License-Identifier: Apache-2.0
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { vi } from 'vitest';

import { findAccessibilityViolations } from '../../../test/axe';
import { adminSession, nasDetails, nasFolder } from '../../../test/fixtures/normal/fixtures';
import { renderApp } from '../../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../../test/server';
import { crumbs, joinPath } from './paths';

// CodeMirror needs a real browser's layout; the end-to-end test drives it.
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

const files = (rest = '') => apiPath(`agents/${nasDetails.id}/files${rest}`);
const base = `/machines/${nasDetails.id}/files?path=${encodeURIComponent('/srv/media')}`;

/** withFolder serves the NAS's /srv/media and records every change. */
function withFolder() {
  signedInAs(adminSession);
  const changes: string[] = [];
  server.use(
    http.get(apiPath(`agents/${nasDetails.id}`), () => json(nasDetails)),
    http.get(files(), () => json(nasFolder)),
    http.post(files('/:action'), async ({ request, params }) => {
      changes.push(`${String(params.action)} ${await request.text()}`);
      return new HttpResponse(null, { status: 204 });
    }),
    http.delete(files(), ({ request }) => {
      changes.push(`delete ${new URL(request.url).search}`);
      return new HttpResponse(null, { status: 204 });
    }),
  );
  return changes;
}

describe('Files tab', () => {
  it('lists a folder, folders first, with downloads and edits offered where they fit', async () => {
    withFolder();
    const user = userEvent.setup();
    const { container } = renderApp(base);
    const list = await screen.findByRole('region', { name: 'Files' });
    await within(list).findByText('3 GB');
    expect(
      within(list)
        .getAllByRole('listitem')
        .map((item) => item.getAttribute('aria-label')),
    ).toEqual(['Photos', 'current', 'backup.tar.gz', 'notes.txt']);
    expect(within(list).getByText('3 GB')).toBeVisible();
    expect(within(list).getByText('→ /srv/media/Photos')).toBeVisible();
    expect(await findAccessibilityViolations(container)).toEqual([]);

    await user.click(within(list).getByRole('button', { name: 'Actions for backup.tar.gz' }));
    expect((await screen.findAllByRole('menuitem')).map((item) => item.textContent)).toEqual([
      'Download',
      'Rename',
      'Delete',
    ]);
    expect(screen.getByRole('menuitem', { name: 'Download' })).toHaveAttribute(
      'href',
      expect.stringContaining('/files/download?path=%2Fsrv%2Fmedia%2Fbackup.tar.gz'),
    );
    await user.keyboard('{Escape}');
    await user.click(within(list).getByRole('button', { name: 'Actions for notes.txt' }));
    expect(await screen.findByRole('menuitem', { name: 'Edit' })).toBeVisible();
  });

  it('opens a folder, and climbs back up by the path', async () => {
    withFolder();
    const user = userEvent.setup();
    const asked: string[] = [];
    server.use(
      http.get(files(), ({ request }) => {
        const path = new URL(request.url).searchParams.get('path') ?? '';
        asked.push(path);
        return json(
          path === '/srv/media'
            ? nasFolder
            : { path, parent: '/srv/media', truncated: false, entries: [] },
        );
      }),
    );
    renderApp(base);
    await user.click(await screen.findByRole('button', { name: 'Photos' }));
    expect(await screen.findByText('This folder is empty.')).toBeVisible();
    await user.click(
      within(screen.getByRole('navigation', { name: 'Folder path' })).getByRole('button', {
        name: 'media',
      }),
    );
    expect(await screen.findByRole('button', { name: 'Photos' })).toBeVisible();
    expect(asked).toEqual(['/srv/media', '/srv/media/Photos', '/srv/media']);
  });

  it('creates, renames and deletes, asking before a delete', async () => {
    const changes = withFolder();
    const user = userEvent.setup();
    renderApp(base);
    await user.click(await screen.findByRole('button', { name: 'New folder' }));
    await user.type(screen.getByRole('textbox', { name: 'Folder name' }), 'Videos');
    await user.click(screen.getByRole('button', { name: 'Create' }));

    await user.click(screen.getByRole('button', { name: 'Actions for notes.txt' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Rename' }));
    const name = screen.getByRole('textbox', { name: 'New name' });
    await user.clear(name);
    await user.type(name, 'a/b');
    expect(screen.getByRole('button', { name: 'Rename' })).toBeDisabled();
    await user.clear(name);
    await user.type(name, 'todo.txt');
    await user.click(screen.getByRole('button', { name: 'Rename' }));

    await user.click(screen.getByRole('button', { name: 'Actions for Photos' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Delete' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Delete Photos?' });
    expect(dialog).toHaveTextContent('everything in it');
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }));
    await waitFor(() => {
      expect(changes).toEqual([
        'mkdir {"path":"/srv/media/Videos"}',
        'rename {"from":"/srv/media/notes.txt","to":"/srv/media/todo.txt"}',
        'delete ?path=%2Fsrv%2Fmedia%2FPhotos&recursive=true',
      ]);
    });
  });

  it('uploads a file in pieces, and offers to replace one that exists', async () => {
    withFolder();
    const user = userEvent.setup();
    const pieces: string[] = [];
    server.use(
      http.post(files('/uploads'), async ({ request }) => {
        const body = (await request.json()) as { path: string; overwrite: boolean };
        if (body.path.endsWith('notes.txt') && !body.overwrite) {
          return apiError(409, 'conflict', 'something with that name is already there');
        }
        return json({ uploadId: 'u1', receivedBytes: 0 });
      }),
      http.put(files('/uploads/u1'), async ({ request }) => {
        pieces.push(
          `${new URL(request.url).searchParams.get('offset') ?? ''}:${String((await request.arrayBuffer()).byteLength)}`,
        );
        return json({ uploadId: 'u1', receivedBytes: 5 });
      }),
      http.post(files('/uploads/u1/finish'), () =>
        json({
          name: 'hello.txt',
          path: '/srv/media/hello.txt',
          type: 'file',
          sizeBytes: 5,
          mode: '-rw-r--r--',
          modified: 0,
        }),
      ),
    );
    renderApp(base);
    await screen.findByRole('region', { name: 'Files' });
    await user.upload(screen.getByLabelText('Files to upload'), [
      new File(['hello'], 'hello.txt'),
      new File(['clash'], 'notes.txt'),
    ]);
    const uploads = await screen.findByRole('list', { name: 'Uploads' });
    expect(await within(uploads).findByText('Uploaded · 5 B')).toBeVisible();
    expect(pieces).toEqual(['0:5']);
    const clash = within(uploads).getByRole('listitem', { name: 'Upload notes.txt' });
    expect(await within(clash).findByText('A file with this name is already there.')).toBeVisible();
    await user.click(within(clash).getByRole('button', { name: 'Replace it' }));
    expect(await within(clash).findByText('Uploaded · 5 B')).toBeVisible();
  });

  it('edits a text file and says when it changed underneath', async () => {
    withFolder();
    const user = userEvent.setup();
    server.use(
      http.get(files('/text'), () =>
        json({ path: '/srv/media/notes.txt', content: 'hello\n', modified: 1 }),
      ),
      http.put(files('/text'), () =>
        apiError(409, 'conflict', 'the file changed since it was opened; reload it first'),
      ),
    );
    renderApp(
      `/machines/${nasDetails.id}/files?path=%2Fsrv%2Fmedia&edit=%2Fsrv%2Fmedia%2Fnotes.txt`,
    );
    const editor = await screen.findByRole('textbox', { name: '/srv/media/notes.txt' });
    await user.type(editor, 'more');
    expect(screen.getByRole('button', { name: 'Close without saving' })).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      /changed since it was opened.*Copy your changes/,
    );
    await user.click(screen.getByRole('button', { name: 'Close without saving' }));
    // Back in the folder the file is in, not at the start.
    expect(await screen.findByRole('button', { name: 'New folder' })).toBeVisible();
  });

  it('says why a folder can’t be listed', async () => {
    withFolder();
    server.use(
      http.get(files(), () =>
        apiError(
          403,
          'policy_denied',
          'this machine’s local policy doesn’t allow the file manager there',
        ),
      ),
    );
    renderApp(base);
    expect(await screen.findByText(/local policy doesn’t allow/)).toBeVisible();
  });

  it('says so when the local policy switches the file manager off', async () => {
    signedInAs(adminSession);
    server.use(
      http.get(apiPath(`agents/${nasDetails.id}`), () =>
        json({
          ...nasDetails,
          capabilities: [{ name: 'files', enabled: false, reason: 'Disabled by local policy' }],
        }),
      ),
    );
    renderApp(base);
    expect(await screen.findByText(/disabled by this machine’s local policy/)).toBeVisible();
  });
});

describe('file paths', () => {
  it.each([
    ['/', [{ name: '/', path: '/' }]],
    [
      '/srv/media',
      [
        { name: '/', path: '/' },
        { name: 'srv', path: '/srv' },
        { name: 'media', path: '/srv/media' },
      ],
    ],
    [
      'C:\\Users\\ali',
      [
        { name: 'C:\\', path: 'C:\\' },
        { name: 'Users', path: 'C:\\Users' },
        { name: 'ali', path: 'C:\\Users\\ali' },
      ],
    ],
    ['', []],
  ])('splits %s into the folders along it', (path, want) => {
    expect(crumbs(path)).toEqual(want);
  });

  it.each([
    ['/srv', 'a', '/srv/a'],
    ['/', 'a', '/a'],
    ['C:\\Users', 'a', 'C:\\Users\\a'],
    ['C:\\', 'a', 'C:\\a'],
  ])('puts %s and %s together as %s', (folder, name, want) => {
    expect(joinPath(folder, name)).toBe(want);
  });
});
