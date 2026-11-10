// SPDX-License-Identifier: Apache-2.0
import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeEach, vi } from 'vitest';

import type { AgentDetails } from '../../api/types.gen';
import { adminSession, nasDetails, viewerSession } from '../../test/fixtures/normal/fixtures';
import { worstMachine } from '../../test/fixtures/worst-case/fixtures';
import { renderApp } from '../../test/renderApp';
import { apiError, apiPath, json, server, signedInAs } from '../../test/server';

/** FakeSocket is the browser's WebSocket to the desktop endpoint. */
const sockets: FakeSocket[] = [];
class FakeSocket {
  static readonly OPEN = 1;
  readyState = 1;
  binaryType = 'blob';
  sent: { type: string; [key: string]: unknown }[] = [];
  onmessage: ((event: { data: string | ArrayBuffer }) => void) | null = null;
  onclose: ((event: { reason: string }) => void) | null = null;
  constructor(readonly url: string) {
    sockets.push(this);
  }
  send(data: string) {
    this.sent.push(JSON.parse(data) as { type: string });
  }
  close() {
    this.readyState = 3;
  }
  control(message: object) {
    this.onmessage?.({ data: JSON.stringify(message) });
  }
  update(bytes: number[]) {
    this.onmessage?.({ data: new Uint8Array(bytes).buffer });
  }
  types() {
    return this.sent.map((message) => message.type);
  }
}

const drawn: { x: number; y: number }[] = [];

const desktopMachine: AgentDetails = {
  ...nasDetails,
  capabilities: [{ name: 'desktop', enabled: true, reason: '' }],
  desktopAvailable: true,
  desktopReason: '',
};

const path = (machine: AgentDetails) => `/machines/${machine.id}/desktop`;

function setUp(
  machine: AgentDetails = desktopMachine,
  preflight: () => Response = () => new HttpResponse(null, { status: 204 }),
) {
  sockets.length = 0;
  drawn.length = 0;
  vi.stubGlobal('WebSocket', FakeSocket);
  signedInAs(adminSession);
  server.use(
    http.get(apiPath(`agents/${machine.id}`), () => json(machine)),
    http.get(apiPath(`agents/${machine.id}/desktop`), preflight),
  );
}

beforeEach(() => {
  // jsdom has no WebRTC, so sessions fall back to the server, and no
  // canvas drawing or image decoding.
  vi.stubGlobal('createImageBitmap', () => Promise.resolve({ close: () => undefined }));
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
    drawImage: (_image: unknown, x: number, y: number) => drawn.push({ x, y }),
  } as unknown as CanvasRenderingContext2D);
  HTMLCanvasElement.prototype.setPointerCapture = () => undefined;
  vi.spyOn(HTMLCanvasElement.prototype, 'getBoundingClientRect').mockReturnValue({
    left: 10,
    top: 20,
    width: 400,
    height: 300,
  } as DOMRect);
});

afterEach(() => {
  vi.restoreAllMocks();
});

async function openSession() {
  await waitFor(() => {
    expect(sockets).toHaveLength(1);
  });
  const socket = sockets[0] as FakeSocket;
  socket.control({ type: 'ready', source: 'test pattern', width: 800, height: 600 });
  socket.update([1, 0x03, 0x20, 0x02, 0x58]);
  return socket;
}

describe('remote desktop', () => {
  it('shows the screen through the server when WebRTC isn’t there, and sends input', async () => {
    setUp();
    renderApp(path(desktopMachine));
    const socket = await openSession();
    expect(await screen.findByText('Through the server · test pattern')).toBeVisible();
    expect(socket.types()).toEqual(['fallback']);

    const canvas = screen.getByLabelText<HTMLCanvasElement>('Screen');
    expect([canvas.width, canvas.height]).toEqual([800, 600]);
    socket.update([2, 0, 64, 0, 128, 0, 64, 0, 64, 0xff, 0xd8]);
    socket.update([3]);
    await waitFor(() => {
      expect(drawn).toEqual([{ x: 64, y: 128 }]);
    });
    expect(socket.types().at(-1)).toBe('ack');

    // The canvas is shown at half size, 10 px in and 20 px down.
    fireEvent.pointerDown(canvas, { clientX: 210, clientY: 170, button: 0, pointerId: 1 });
    fireEvent.pointerUp(canvas, { clientX: 210, clientY: 170, button: 0, pointerId: 1 });
    fireEvent.pointerMove(canvas, { clientX: 10, clientY: 20 });
    fireEvent.wheel(canvas, { clientX: 10, clientY: 20, deltaY: 120 });
    fireEvent.keyDown(canvas, { code: 'KeyA', key: 'a' });
    fireEvent.keyUp(canvas, { code: 'KeyA', key: 'a' });
    expect(fireEvent.contextMenu(canvas)).toBe(false);
    const inputs = socket.sent.filter((message) => message.type === 'input').map((m) => m.input);
    expect(inputs).toEqual([
      { kind: 'mousedown', button: 0, x: 400, y: 300 },
      { kind: 'mouseup', button: 0, x: 400, y: 300 },
      { kind: 'mousemove', x: 0, y: 0 },
      { kind: 'wheel', deltaX: 0, deltaY: 1, x: 0, y: 0 },
      { kind: 'keydown', code: 'KeyA', key: 'a' },
      { kind: 'keyup', code: 'KeyA', key: 'a' },
    ]);
    // Ctrl+Alt+Del is only for Windows.
    expect(screen.queryByRole('button', { name: 'Ctrl+Alt+Del' })).not.toBeInTheDocument();
  });

  it('changes the picture’s quality', async () => {
    setUp();
    renderApp(path(desktopMachine));
    const socket = await openSession();
    const slider = await screen.findByRole('slider', { name: /Quality/ });
    fireEvent.change(slider, { target: { value: '40' } });
    expect(socket.sent.at(-1)).toEqual({ type: 'quality', quality: 40 });
    expect(screen.getByText('40')).toBeVisible();
  });

  it('starts again view only, ignoring the mouse and keys', async () => {
    setUp();
    const user = userEvent.setup();
    renderApp(path(desktopMachine));
    await openSession();
    await user.click(screen.getByRole('checkbox', { name: 'View only' }));
    await waitFor(() => {
      expect(sockets).toHaveLength(2);
    });
    const socket = sockets[1] as FakeSocket;
    expect(socket.url).toMatch(/\/desktop\?viewOnly=true$/);
    socket.control({ type: 'ready' });
    const canvas = screen.getByLabelText('Screen');
    fireEvent.pointerDown(canvas, { clientX: 20, clientY: 30 });
    fireEvent.keyDown(canvas, { code: 'KeyA', key: 'a' });
    expect(socket.types()).toEqual(['fallback']);
    expect(canvas).not.toHaveAttribute('tabindex');
  });

  it('starts again relayed, without trying WebRTC', async () => {
    setUp();
    const user = userEvent.setup();
    renderApp(path(desktopMachine));
    await openSession();
    await user.click(screen.getByRole('checkbox', { name: 'Relay through the server' }));
    await waitFor(() => {
      expect(sockets).toHaveLength(2);
    });
    expect(screen.getByRole('checkbox', { name: 'Relay through the server' })).toBeChecked();
  });

  it('offers Ctrl+Alt+Del on Windows', async () => {
    const windows = { ...desktopMachine, os: 'windows' };
    setUp(windows);
    const user = userEvent.setup();
    renderApp(path(windows));
    const socket = await openSession();
    await user.click(await screen.findByRole('button', { name: 'Ctrl+Alt+Del' }));
    expect(socket.sent.at(-1)).toEqual({ type: 'input', input: { kind: 'ctrlaltdel' } });
  });

  it('ends the session on request, and starts a new one', async () => {
    setUp();
    const user = userEvent.setup();
    renderApp(path(desktopMachine));
    await openSession();
    await user.click(await screen.findByRole('button', { name: 'End session' }));
    expect(screen.getByText('You closed the session.')).toBeVisible();
    expect(sockets[0]?.readyState).toBe(3);
    await user.click(screen.getByRole('button', { name: 'New session' }));
    await waitFor(() => {
      expect(sockets).toHaveLength(2);
    });
  });

  it('shows why the server ended the session', async () => {
    setUp();
    renderApp(path(desktopMachine));
    const socket = await openSession();
    socket.onclose?.({ reason: 'The machine went offline.' });
    expect(await screen.findByText('The machine went offline.')).toBeVisible();
  });

  it('shows why it couldn’t open', async () => {
    setUp(desktopMachine, () => apiError(409, 'desktop_unavailable', 'Nobody is signed in.'));
    renderApp(path(desktopMachine));
    expect(await screen.findByText('Remote desktop couldn’t be opened.')).toBeVisible();
    expect(screen.getByText('Nobody is signed in.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'New session' })).toBeVisible();
  });

  it('SEC-07: asks for the second factor first, then opens', async () => {
    let checked = false;
    setUp(desktopMachine, () =>
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
    renderApp(path(desktopMachine));
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

  it('says so when the second factor isn’t confirmed', async () => {
    setUp(desktopMachine, () => apiError(403, 'step_up_required', 'Confirm it’s you first.'));
    const user = userEvent.setup();
    renderApp(path(desktopMachine));
    await screen.findByRole('dialog', { name: 'Confirm it’s you' });
    await user.keyboard('{Escape}');
    expect(
      await screen.findByText('Remote desktop needs a fresh check of your second factor.'),
    ).toBeVisible();
  });

  it.each([
    {
      name: 'says when the machine is offline',
      machine: { ...desktopMachine, online: false },
      text: /machine is offline/,
    },
    {
      name: 'says why the desktop can’t be shared',
      machine: { ...worstMachine, online: true },
      text: /Wayland sessions aren’t supported yet/,
    },
    {
      name: 'gives a reason when the agent gives none',
      machine: { ...desktopMachine, desktopAvailable: false },
      text: 'Remote desktop isn’t available on this machine.',
    },
  ])('$name', async ({ machine, text }) => {
    setUp(machine);
    renderApp(path(machine));
    expect(await screen.findByText(text)).toBeVisible();
    expect(sockets).toHaveLength(0);
  });
});

describe('the Desktop tab', () => {
  it.each([
    {
      name: 'is shown to operators where the policy allows it',
      session: adminSession,
      machine: desktopMachine,
      shown: true,
    },
    {
      name: 'is hidden where the policy turns it off',
      session: adminSession,
      machine: nasDetails,
      shown: false,
    },
    {
      name: 'is hidden while there’s no desktop to share',
      session: adminSession,
      machine: { ...desktopMachine, desktopAvailable: false },
      shown: false,
    },
    {
      name: 'is hidden from viewers',
      session: viewerSession,
      machine: desktopMachine,
      shown: false,
    },
  ])('$name', async ({ session, machine, shown }) => {
    signedInAs(session);
    server.use(http.get(apiPath(`agents/${machine.id}`), () => json(machine)));
    renderApp(`/machines/${machine.id}`);
    await screen.findByRole('link', { name: 'Overview' });
    expect(screen.queryByRole('link', { name: 'Desktop' }) !== null).toBe(shown);
  });
});
