// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest';

import {
  DesktopConnection,
  machineDesktop,
  screenPoint,
  webRTCTimeoutMs,
  type DecodedTile,
  type DesktopDeps,
  type DesktopOptions,
  type DesktopState,
} from './desktop';
import { ApiError } from './client';

/** FakeSocket is the browser's WebSocket to the desktop endpoint. */
class FakeSocket {
  readyState: number = WebSocket.OPEN;
  binaryType = 'blob';
  sent: string[] = [];
  onmessage: ((event: { data: string | ArrayBuffer }) => void) | null = null;
  onclose: ((event: { reason: string }) => void) | null = null;
  closed = false;
  constructor(readonly url: string) {}
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.closed = true;
    this.readyState = WebSocket.CLOSED;
  }
  control(message: object | string) {
    this.onmessage?.({ data: typeof message === 'string' ? message : JSON.stringify(message) });
  }
  update(bytes: number[]) {
    this.onmessage?.({ data: new Uint8Array(bytes).buffer });
  }
  types(): string[] {
    return this.sent.map((s) => (JSON.parse(s) as { type: string }).type);
  }
}

/** FakeChannel is a WebRTC data channel. */
class FakeChannel {
  readyState = 'connecting';
  binaryType = 'blob';
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null;
  constructor(readonly label: string) {}
  send(data: string) {
    this.sent.push(data);
  }
  open() {
    this.readyState = 'open';
    this.onopen?.();
  }
  update(bytes: number[]) {
    this.onmessage?.({ data: new Uint8Array(bytes).buffer });
  }
}

/** FakePeer is an RTCPeerConnection. */
class FakePeer {
  channels = new Map<string, FakeChannel>();
  remote: unknown[] = [];
  candidates: unknown[] = [];
  closed = false;
  onicecandidate: ((event: { candidate: { toJSON: () => object } | null }) => void) | null = null;
  createDataChannel(label: string) {
    const channel = new FakeChannel(label);
    this.channels.set(label, channel);
    return channel;
  }
  createOffer() {
    return Promise.resolve({ type: 'offer', sdp: 'v=0 offer' });
  }
  setLocalDescription() {
    return Promise.resolve();
  }
  setRemoteDescription(description: unknown) {
    this.remote.push(description);
    return Promise.resolve();
  }
  addIceCandidate(candidate: unknown) {
    this.candidates.push(candidate);
    return Promise.reject(new Error('a candidate the browser can’t use'));
  }
  close() {
    this.closed = true;
  }
  channel(label: string): FakeChannel {
    const found = this.channels.get(label);
    if (found === undefined) {
      throw new Error(`no ${label} channel`);
    }
    return found;
  }
}

const fakeImage = (name: string) => ({ name, close: () => undefined }) as unknown as ImageBitmap;

interface HarnessOptions extends Partial<DesktopOptions> {
  preflight?: Error;
  noPeer?: boolean;
  badImages?: boolean;
}

function harness(options: HarnessOptions = {}) {
  const sockets: FakeSocket[] = [];
  const peers: FakePeer[] = [];
  const states: DesktopState[] = [];
  const frames: DecodedTile[][] = [];
  const sizes: [number, number][] = [];
  const timers: (() => void)[] = [];
  const preflights: string[] = [];
  const deps: DesktopDeps = {
    preflight: (path) => {
      preflights.push(path);
      return options.preflight === undefined
        ? Promise.resolve()
        : Promise.reject(options.preflight);
    },
    createSocket: (url) => {
      const socket = new FakeSocket(url);
      sockets.push(socket);
      return socket as unknown as WebSocket;
    },
    createPeer: () => {
      if (options.noPeer === true) {
        throw new Error('WebRTC is turned off');
      }
      const peer = new FakePeer();
      peers.push(peer);
      return peer as unknown as RTCPeerConnection;
    },
    decodeImage: (jpeg) =>
      options.badImages === true
        ? Promise.reject(new Error('not a JPEG'))
        : Promise.resolve(fakeImage(String.fromCharCode(...jpeg))),
    setTimeout: (callback) => timers.push(callback),
    clearTimeout: () => undefined,
  };
  const connection = new DesktopConnection(
    machineDesktop('01JB X'),
    { viewOnly: options.viewOnly ?? false, relayOnly: options.relayOnly ?? false },
    {
      onState: (state) => states.push(state),
      onResize: (width, height) => sizes.push([width, height]),
      onFrame: (tiles) => frames.push(tiles),
    },
    deps,
  );
  const socket = () => {
    const found = sockets[0];
    if (found === undefined) {
      throw new Error('no socket');
    }
    return found;
  };
  const peer = () => {
    const found = peers[0];
    if (found === undefined) {
      throw new Error('no peer');
    }
    return found;
  };
  return {
    connection,
    sockets,
    socket,
    peers,
    peer,
    states,
    frames,
    sizes,
    timers,
    preflights,
    last: () => states.at(-1),
  };
}

/** settle lets promise callbacks run. */
const settle = async () => {
  for (let i = 0; i < 5; i++) {
    await Promise.resolve();
  }
};

// An 800×600 screen, one tile "AB" at (64, 128), and the frame's end.
const screenUpdate = [1, 0x03, 0x20, 0x02, 0x58];
const tileUpdate = [2, 0, 64, 0, 128, 0, 64, 0, 64, 65, 66];
const endUpdate = [3];

describe('machineDesktop', () => {
  it('names a machine’s desktop endpoint', () => {
    expect(machineDesktop('a b')).toBe('api/agents/a%20b/desktop');
  });
});

describe('screenPoint', () => {
  it.each([
    {
      name: 'maps a scaled-down screen back to its pixels',
      x: 200,
      y: 150,
      want: { x: 400, y: 300 },
    },
    {
      name: 'takes the pixel a point falls inside, not the nearest edge',
      x: 200.4,
      y: 150.9,
      want: { x: 400, y: 301 },
    },
    { name: 'keeps the top left corner', x: 0, y: 0, want: { x: 0, y: 0 } },
    {
      name: 'clamps points past the edge to the last pixel',
      x: 500,
      y: 400,
      want: { x: 799, y: 599 },
    },
    { name: 'clamps points before the edge to zero', x: -5, y: -1, want: { x: 0, y: 0 } },
  ])('$name', ({ x, y, want }) => {
    expect(screenPoint(x, y, { width: 400, height: 300 }, { width: 800, height: 600 })).toEqual(
      want,
    );
  });

  it('gives the corner while the canvas isn’t shown', () => {
    expect(screenPoint(10, 10, { width: 0, height: 0 }, { width: 800, height: 600 })).toEqual({
      x: 0,
      y: 0,
    });
  });
});

describe('DesktopConnection', () => {
  it('checks first, then opens the WebSocket next to the page', async () => {
    const h = harness();
    await h.connection.start();
    expect(h.preflights).toEqual(['api/agents/01JB%20X/desktop']);
    expect(h.socket().url).toBe('ws://localhost:3000/api/agents/01JB%20X/desktop');
    expect(h.socket().binaryType).toBe('arraybuffer');
    expect(h.states).toEqual([{ kind: 'connecting' }]);
  });

  it('asks for a view-only session', async () => {
    const h = harness({ viewOnly: true });
    await h.connection.start();
    expect(h.socket().url).toContain('/desktop?viewOnly=true');
  });

  it('throws a refusal, opening nothing', async () => {
    const refusal = new ApiError(403, { error: { code: 'step_up_required', message: 'Confirm.' } });
    const h = harness({ preflight: refusal });
    await expect(h.connection.start()).rejects.toBe(refusal);
    expect(h.sockets).toHaveLength(0);
  });

  it('opens nothing when closed during the check', async () => {
    const h = harness();
    const started = h.connection.start();
    h.connection.close();
    await started;
    expect(h.sockets).toHaveLength(0);
    expect(h.last()).toEqual({ kind: 'ended', reason: 'You closed the session.' });
  });

  it('connects directly over WebRTC and draws frames from the data channel', async () => {
    const h = harness();
    await h.connection.start();
    h.socket().control({ type: 'ready', source: 'test pattern', width: 800, height: 600 });
    await settle();
    expect(JSON.parse(h.socket().sent[0] ?? '')).toEqual({ type: 'offer', sdp: 'v=0 offer' });

    h.peer().onicecandidate?.({ candidate: { toJSON: () => ({ candidate: 'candidate:1' }) } });
    h.peer().onicecandidate?.({ candidate: null });
    expect(JSON.parse(h.socket().sent[1] ?? '')).toEqual({
      type: 'candidate',
      candidate: { candidate: 'candidate:1' },
    });
    expect(h.socket().sent).toHaveLength(2);

    h.socket().control({ type: 'answer', sdp: 'v=0 answer' });
    h.socket().control({ type: 'candidate', candidate: { candidate: 'candidate:2' } });
    h.socket().control({ type: 'candidate' });
    await settle();
    expect(h.peer().remote).toEqual([{ type: 'answer', sdp: 'v=0 answer' }]);
    expect(h.peer().candidates).toEqual([{ candidate: 'candidate:2' }]);

    const frames = h.peer().channel('frames');
    frames.open();
    expect(h.last()).toEqual({
      kind: 'open',
      transport: 'direct',
      source: 'test pattern',
      width: 0,
      height: 0,
    });
    frames.update(screenUpdate);
    expect(h.sizes).toEqual([[800, 600]]);
    expect(h.last()).toMatchObject({ transport: 'direct', width: 800, height: 600 });
    frames.update(tileUpdate);
    expect(h.frames).toHaveLength(0);
    frames.update(endUpdate);
    await settle();
    expect(h.frames).toHaveLength(1);
    expect(h.frames[0]?.[0]).toMatchObject({ x: 64, y: 128, image: { name: 'AB' } });
    // Frames on the data channel aren't acknowledged.
    expect(h.socket().types()).not.toContain('ack');

    // The fallback timer, firing late, changes nothing.
    h.timers[0]?.();
    expect(h.socket().types()).not.toContain('fallback');
  });

  it('sends input on the data channel once direct, else through the server', async () => {
    const h = harness();
    await h.connection.start();
    h.connection.send({ kind: 'mousemove', x: 1, y: 2 });
    h.socket().control({ type: 'ready' });
    await settle();
    h.peer().channel('frames').open();
    const input = h.peer().channel('input');
    // Until the input channel opens, input still goes through the server.
    h.connection.send({ kind: 'keydown', code: 'KeyA', key: 'a' });
    expect(h.socket().types()).toEqual(['input', 'offer', 'input']);
    input.readyState = 'open';
    h.connection.send({ kind: 'mousedown', x: 3, y: 4, button: 0 });
    expect(input.sent.map((s) => JSON.parse(s) as unknown)).toEqual([
      { type: 'input', input: { kind: 'mousedown', x: 3, y: 4, button: 0 } },
    ]);
  });

  it('sends candidates found before the offer after it', async () => {
    const h = harness();
    await h.connection.start();
    h.socket().control({ type: 'ready' });
    h.peer().onicecandidate?.({ candidate: { toJSON: () => ({ candidate: 'candidate:0' }) } });
    await settle();
    expect(h.socket().types()).toEqual(['offer', 'candidate']);
  });

  it('falls back to the server when WebRTC doesn’t connect in time', async () => {
    const h = harness();
    await h.connection.start();
    h.socket().control({ type: 'ready', source: 'X11 :0' });
    await settle();
    expect(h.timers).toHaveLength(1);
    h.timers[0]?.();
    expect(h.peer().closed).toBe(true);
    expect(h.socket().types()).toEqual(['offer', 'fallback']);
    expect(h.last()).toEqual({
      kind: 'open',
      transport: 'relayed',
      source: 'X11 :0',
      width: 0,
      height: 0,
    });

    // A channel opening afterwards is ignored.
    h.peer().channel('frames').open();
    expect(h.last()).toMatchObject({ transport: 'relayed' });

    h.socket().update(screenUpdate);
    h.socket().update(tileUpdate);
    h.socket().update(endUpdate);
    await settle();
    expect(h.frames).toHaveLength(1);
    expect(h.socket().types().at(-1)).toBe('ack');
  });

  it.each([
    { name: 'falls back at once when asked to relay', options: { relayOnly: true } },
    { name: 'falls back when WebRTC can’t start in this browser', options: { noPeer: true } },
  ])('$name', async ({ options }) => {
    const h = harness(options);
    await h.connection.start();
    h.socket().control({ type: 'ready' });
    await settle();
    expect(h.peers).toHaveLength(0);
    expect(h.socket().types()).toEqual(['fallback']);
    expect(h.last()).toMatchObject({ kind: 'open', transport: 'relayed' });
  });

  it('falls back when the agent can’t answer', async () => {
    const h = harness();
    await h.connection.start();
    h.socket().control({ type: 'ready' });
    await settle();
    h.socket().control({ type: 'error', message: 'no WebRTC' });
    expect(h.socket().types()).toEqual(['offer', 'fallback']);
    // A second error doesn't fall back twice.
    h.socket().control({ type: 'error' });
    expect(h.socket().types()).toEqual(['offer', 'fallback']);
  });

  it('asks for a whole screen when a tile can’t be decoded', async () => {
    const h = harness({ relayOnly: true, badImages: true });
    await h.connection.start();
    h.socket().control({ type: 'ready' });
    h.socket().update(tileUpdate);
    h.socket().update(endUpdate);
    await settle();
    expect(h.frames).toHaveLength(0);
    expect(h.socket().types()).toEqual(['fallback', 'keyframe', 'ack']);
  });

  it('asks for a whole screen without acknowledging a direct frame it can’t decode', async () => {
    const h = harness({ badImages: true });
    await h.connection.start();
    h.socket().control({ type: 'ready' });
    await settle();
    const frames = h.peer().channel('frames');
    frames.open();
    frames.update(tileUpdate);
    frames.update(endUpdate);
    await settle();
    expect(h.socket().types()).toEqual(['offer', 'keyframe']);
  });

  it('ignores messages it can’t read and kinds it doesn’t know', async () => {
    const h = harness();
    await h.connection.start();
    h.socket().control('{not json');
    h.socket().control({ type: 'hello' });
    h.socket().update([9, 1, 2]);
    await settle();
    expect(h.socket().sent).toHaveLength(0);
    expect(h.states).toEqual([{ kind: 'connecting' }]);
  });

  it('drops input in a view-only session but still sets the quality', async () => {
    const h = harness({ viewOnly: true, relayOnly: true });
    await h.connection.start();
    h.connection.send({ kind: 'ctrlaltdel' });
    h.connection.setQuality(40);
    expect(h.socket().sent.map((s) => JSON.parse(s) as unknown)).toEqual([
      { type: 'quality', quality: 40 },
    ]);
  });

  it('sends nothing once the socket has closed', async () => {
    const h = harness();
    await h.connection.start();
    h.socket().readyState = WebSocket.CLOSED;
    h.connection.setQuality(40);
    expect(h.socket().sent).toHaveLength(0);
  });

  it('ends with the server’s reason, once', async () => {
    const h = harness();
    await h.connection.start();
    h.socket().control({ type: 'ready' });
    await settle();
    h.socket().onclose?.({ reason: 'The machine went offline.' });
    h.socket().onclose?.({ reason: 'again' });
    expect(h.peer().closed).toBe(true);
    expect(h.socket().closed).toBe(true);
    expect(h.states.filter((s) => s.kind === 'ended')).toEqual([
      { kind: 'ended', reason: 'The machine went offline.' },
    ]);
  });

  it('ends with a plain reason when the server gives none', async () => {
    const h = harness();
    await h.connection.start();
    h.socket().onclose?.({ reason: '' });
    expect(h.last()).toEqual({ kind: 'ended', reason: 'The session ended.' });
  });

  it('closes the socket when the user ends the session', async () => {
    const h = harness();
    await h.connection.start();
    h.connection.close();
    expect(h.socket().closed).toBe(true);
    expect(h.last()).toEqual({ kind: 'ended', reason: 'You closed the session.' });
  });

  it('gives WebRTC eight seconds', () => {
    expect(webRTCTimeoutMs).toBe(8000);
  });
});
