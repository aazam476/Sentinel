// SPDX-License-Identifier: Apache-2.0
import { describe, expect, it } from 'vitest';

import { ApiError } from './client';
import { TerminalConnection, type TerminalDeps, type TerminalState } from './terminal';

/** FakeSocket is a WebSocket the test drives. */
class FakeSocket extends EventTarget {
  readyState: number = WebSocket.CONNECTING;
  binaryType = 'blob';
  sent: (string | Uint8Array)[] = [];
  constructor(readonly url: string) {
    super();
  }
  send(data: string | Uint8Array) {
    this.sent.push(data);
  }
  close() {
    this.readyState = WebSocket.CLOSED;
  }
  open() {
    this.readyState = WebSocket.OPEN;
  }
  status(status: object) {
    this.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(status) }));
  }
  output(text: string) {
    this.dispatchEvent(
      new MessageEvent('message', { data: new TextEncoder().encode(text).buffer }),
    );
  }
  drop(reason = '') {
    this.readyState = WebSocket.CLOSED;
    this.dispatchEvent(new CloseEvent('close', { code: 1006, reason }));
  }
}

/** harness runs a TerminalConnection on fake sockets, time and checks. */
function harness(preflightAnswers: (Error | undefined)[] = []) {
  const sockets: FakeSocket[] = [];
  const states: TerminalState[] = [];
  const output: string[] = [];
  const preflights: string[] = [];
  const timers: { at: number; run: () => void }[] = [];
  let now = 0;
  const deps: TerminalDeps = {
    createSocket: (url) => {
      const socket = new FakeSocket(url);
      sockets.push(socket);
      return socket as unknown as WebSocket;
    },
    preflight: (path) => {
      preflights.push(path);
      const answer = preflightAnswers.shift();
      return answer === undefined ? Promise.resolve() : Promise.reject(answer);
    },
    now: () => now,
    setTimeout: (run, ms) => timers.push({ at: now + ms, run }),
    clearTimeout: () => undefined,
  };
  const connection = new TerminalConnection(
    '01JB X',
    { cols: 120, rows: 40 },
    {
      onOutput: (data) => output.push(new TextDecoder().decode(data)),
      onState: (state) => states.push(state),
    },
    deps,
  );
  /** advance moves time on, running due timers and waiting for their work. */
  const advance = async (ms: number) => {
    now += ms;
    for (const timer of timers.splice(0).filter((t) => t.at <= now)) {
      timer.run();
    }
    await Promise.resolve();
    await Promise.resolve();
  };
  /** socket returns the nth socket opened, failing the test if there isn't one. */
  const socket = (n: number): FakeSocket => {
    const found = sockets[n];
    if (found === undefined) {
      throw new Error(`socket ${String(n)} was never opened`);
    }
    return found;
  };
  return {
    connection,
    sockets,
    socket,
    states,
    output,
    preflights,
    advance,
    last: () => states.at(-1),
  };
}

const offline = new ApiError(409, {
  error: { code: 'machine_offline', message: 'The machine is offline.' },
});

describe('TerminalConnection', () => {
  it('checks first, then relays output, keystrokes and resizes', async () => {
    const h = harness();
    await h.connection.start();
    expect(h.preflights).toEqual(['api/agents/01JB%20X/terminal?cols=120&rows=40']);
    const socket = h.socket(0);
    expect(socket.url).toMatch(/^ws:\/\/.*\/api\/agents\/01JB%20X\/terminal\?cols=120&rows=40$/);
    expect(socket.binaryType).toBe('arraybuffer');
    socket.open();
    socket.status({ type: 'opened', sessionId: 's1' });
    expect(h.last()).toEqual({ kind: 'open', resumed: false });
    socket.output('$ ');
    expect(h.output).toEqual(['$ ']);
    h.connection.send('ls\r');
    h.connection.resize({ cols: 80, rows: 24 });
    expect(new TextDecoder().decode(socket.sent[0] as Uint8Array)).toBe('ls\r');
    expect(socket.sent[1]).toBe('{"type":"resize","cols":80,"rows":24}');
    socket.status({ type: 'closed', reason: 'the shell exited' });
    socket.drop();
    expect(h.last()).toEqual({ kind: 'ended', reason: 'the shell exited' });
  });

  it('passes on the refusal from the check', async () => {
    const stepUp = new ApiError(403, {
      error: { code: 'step_up_required', message: 'Confirm it’s you.' },
    });
    const h = harness([stepUp]);
    await expect(h.connection.start()).rejects.toBe(stepUp);
    expect(h.sockets).toHaveLength(0);
  });

  it('shows the agent’s reason when the shell never opened', async () => {
    const h = harness();
    await h.connection.start();
    h.socket(0).drop('The terminal is disabled by this machine’s local policy.');
    expect(h.last()).toEqual({
      kind: 'ended',
      reason: 'The terminal is disabled by this machine’s local policy.',
    });
  });

  it('reconnects to the same shell after a drop, while the machine comes back', async () => {
    const h = harness([undefined, offline]);
    await h.connection.start();
    h.socket(0).open();
    h.socket(0).status({ type: 'opened', sessionId: 's1' });
    h.socket(0).drop();
    expect(h.last()).toEqual({ kind: 'reconnecting', secondsLeft: 30 });
    await h.advance(1000); // the machine is still offline
    expect(h.last()).toEqual({ kind: 'reconnecting', secondsLeft: 29 });
    await h.advance(2000);
    expect(h.sockets).toHaveLength(2);
    expect(h.socket(1).url).toContain('resume=s1');
    h.socket(1).open();
    h.socket(1).status({ type: 'opened', sessionId: 's1', resumed: true });
    expect(h.last()).toEqual({ kind: 'open', resumed: true });
  });

  it('gives up when the shell has ended or 30 seconds have passed', async () => {
    const gone = new ApiError(404, {
      error: { code: 'not_found', message: 'That terminal session has ended.' },
    });
    const h = harness([undefined, gone]);
    await h.connection.start();
    h.socket(0).status({ type: 'opened', sessionId: 's1' });
    h.socket(0).drop();
    await h.advance(1000);
    expect(h.last()).toEqual({ kind: 'ended', reason: 'That terminal session has ended.' });

    const slow = harness([undefined, ...Array<ApiError>(20).fill(offline)]);
    await slow.connection.start();
    slow.socket(0).status({ type: 'opened', sessionId: 's1' });
    slow.socket(0).drop();
    for (let i = 0; i < 10; i++) {
      await slow.advance(5000);
    }
    expect(slow.last()).toEqual({
      kind: 'ended',
      reason: 'The connection was lost and couldn’t be restored.',
    });
  });

  it('stops reconnecting at a refusal waiting won’t fix', async () => {
    const stepUp = new ApiError(403, {
      error: { code: 'step_up_required', message: 'Confirm it’s you.' },
    });
    const h = harness([undefined, stepUp]);
    await h.connection.start();
    h.socket(0).status({ type: 'opened', sessionId: 's1' });
    h.socket(0).drop();
    await h.advance(1000);
    expect(h.last()).toEqual({ kind: 'failed', error: stepUp });
  });

  it('ends the shell when closed, and leaves it running when detached', async () => {
    const h = harness();
    await h.connection.start();
    h.socket(0).open();
    h.connection.close();
    expect(h.socket(0).sent).toEqual(['{"type":"close"}']);

    const left = harness();
    await left.connection.start();
    left.socket(0).open();
    left.connection.detach();
    expect(left.socket(0).sent).toEqual([]);
    left.socket(0).drop();
    expect(left.last()).toEqual({ kind: 'connecting' }); // no reconnecting after leaving
  });

  it('ignores status messages it can’t read', async () => {
    const h = harness();
    await h.connection.start();
    h.socket(0).dispatchEvent(new MessageEvent('message', { data: 'not json' }));
    expect(h.last()).toEqual({ kind: 'connecting' });
  });
});
