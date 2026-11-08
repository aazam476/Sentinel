// SPDX-License-Identifier: Apache-2.0
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { vi } from 'vitest';

import { adminSession, nasDetails } from '../../test/fixtures/normal/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

/** FakeTerminal stands in for xterm.js, which needs a real canvas. */
const terminals: FakeTerminal[] = [];
class FakeTerminal {
  cols = 80;
  rows = 24;
  written: string[] = [];
  disposed = false;
  private dataListener: ((data: string) => void) | undefined;
  constructor() {
    terminals.push(this);
  }
  loadAddon() {}
  open() {}
  focus() {}
  write(data: Uint8Array) {
    this.written.push(new TextDecoder().decode(data));
  }
  onData(listener: (data: string) => void) {
    this.dataListener = listener;
    return { dispose: () => undefined };
  }
  onResize() {
    return { dispose: () => undefined };
  }
  dispose() {
    this.disposed = true;
  }
  type(data: string) {
    this.dataListener?.(data);
  }
}

vi.mock('@xterm/xterm', () => ({ Terminal: FakeTerminal }));
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {}
  },
}));

/** FakeSocket is the browser's WebSocket to the terminal endpoint. */
const sockets: FakeSocket[] = [];
class FakeSocket extends EventTarget {
  static readonly OPEN = 1;
  readyState = 1;
  binaryType = 'blob';
  sent: unknown[] = [];
  constructor(readonly url: string) {
    super();
    sockets.push(this);
  }
  send(data: unknown) {
    this.sent.push(typeof data === 'string' ? data : new TextDecoder().decode(data as Uint8Array));
  }
  close() {
    this.readyState = 3;
  }
  status(status: object) {
    this.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(status) }));
  }
  output(text: string) {
    this.dispatchEvent(
      new MessageEvent('message', { data: new TextEncoder().encode(text).buffer }),
    );
  }
}

const path = `/machines/${nasDetails.id}/terminal`;

function setUp(preflight: () => Response = () => new HttpResponse(null, { status: 204 })) {
  terminals.length = 0;
  sockets.length = 0;
  vi.stubGlobal('WebSocket', FakeSocket);
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  signedInAs(adminSession);
  server.use(
    http.get(apiPath(`agents/${nasDetails.id}`), () => json(nasDetails)),
    http.get(apiPath(`agents/${nasDetails.id}/terminal`), preflight),
  );
}

describe('terminal', () => {
  it('opens a shell, shows its output and sends what is typed', async () => {
    setUp();
    renderApp(path);
    await waitFor(() => {
      expect(sockets).toHaveLength(1);
    });
    const socket = sockets[0];
    expect(socket?.url).toMatch(/\/api\/agents\/.*\/terminal\?cols=80&rows=24$/);
    socket?.status({ type: 'opened', sessionId: 's1' });
    expect(await screen.findByText('Connected')).toBeVisible();
    socket?.output('root@nas:~# ');
    expect(terminals[0]?.written).toEqual(['root@nas:~# ']);
    terminals[0]?.type('ls\r');
    expect(socket?.sent).toEqual(['ls\r']);
  });

  it('sends a phone’s missing keys, and ends the session on request', async () => {
    setUp();
    const user = userEvent.setup();
    renderApp(path);
    await waitFor(() => {
      expect(sockets).toHaveLength(1);
    });
    sockets[0]?.status({ type: 'opened', sessionId: 's1' });
    await user.click(await screen.findByRole('button', { name: 'Ctrl-C' }));
    await user.click(screen.getByRole('button', { name: 'Up' }));
    await user.click(screen.getByRole('button', { name: 'End session' }));
    expect(sockets[0]?.sent).toEqual(['\x03', '\x1b[A', '{"type":"close"}']);
    expect(screen.getByText('You closed the session.')).toBeVisible();

    await user.click(screen.getByRole('button', { name: 'New session' }));
    await waitFor(() => {
      expect(sockets).toHaveLength(2);
    });
    expect(terminals[0]?.disposed).toBe(true);
  });

  it('SEC-07: asks for the second factor first, then opens', async () => {
    let checked = false;
    setUp(() =>
      checked
        ? new HttpResponse(null, { status: 204 })
        : apiError(403, 'step_up_required', 'Confirm it’s you first.'),
    );
    server.use(
      http.post(apiPath('auth/step-up'), () => {
        checked = true;
        return json(adminSession);
      }),
    );
    const user = userEvent.setup();
    renderApp(path);
    const dialog = await screen.findByRole('dialog', { name: 'Confirm it’s you' });
    expect(sockets).toHaveLength(0);
    await user.type(await screen.findByRole('textbox', { name: /code/i }), '123456');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(dialog).not.toBeInTheDocument();
    });
    await waitFor(() => {
      expect(sockets).toHaveLength(1);
    });
  });

  it('shows why the shell ended', async () => {
    setUp();
    renderApp(path);
    await waitFor(() => {
      expect(sockets).toHaveLength(1);
    });
    sockets[0]?.status({ type: 'opened', sessionId: 's1' });
    sockets[0]?.status({ type: 'closed', reason: 'the shell exited' });
    expect(await screen.findByText('the shell exited')).toBeVisible();
    expect(screen.getByRole('button', { name: 'New session' })).toBeVisible();
  });

  it('says when the machine is offline', async () => {
    setUp();
    server.use(
      http.get(apiPath(`agents/${nasDetails.id}`), () => json({ ...nasDetails, online: false })),
    );
    renderApp(path);
    expect(await screen.findByText(/The machine is offline/)).toBeVisible();
  });
});
