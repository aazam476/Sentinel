// SPDX-License-Identifier: Apache-2.0
import { apiRequest, apiURL, isApiError } from './client';

/** resumeWindowMs is how long the agent keeps a shell whose viewer dropped. */
export const resumeWindowMs = 30_000;

/** TerminalSize is a terminal's size in character cells. */
export interface TerminalSize {
  cols: number;
  rows: number;
}

/**
 * ServerStatus is a text message from the server, mirroring relay.Status in
 * internal/server/relay/relay.go (not an API type, so it isn't generated).
 */
interface ServerStatus {
  type: 'opened' | 'closed';
  sessionId?: string;
  resumed?: boolean;
  reason?: string;
}

/** TerminalState is what the terminal shows around the shell. */
export type TerminalState =
  | { kind: 'connecting' }
  | { kind: 'open'; resumed: boolean }
  | { kind: 'reconnecting'; secondsLeft: number }
  | { kind: 'ended'; reason: string }
  | { kind: 'failed'; error: unknown };

export interface TerminalCallbacks {
  onOutput: (data: Uint8Array) => void;
  onState: (state: TerminalState) => void;
}

/** refusals end reconnecting: waiting won't change them. */
const refusals = new Set(['step_up_required', 'forbidden', 'policy_denied', 'not_authenticated']);

/** Dependencies, replaced in tests. */
export interface TerminalDeps {
  createSocket: (url: string) => WebSocket;
  /** preflight runs the server's checks without opening a shell. */
  preflight: (path: string) => Promise<unknown>;
  now: () => number;
  setTimeout: (callback: () => void, ms: number) => number;
  clearTimeout: (id: number) => void;
}

const defaultDeps: TerminalDeps = {
  createSocket: (url) => new WebSocket(url),
  preflight: (path) => apiRequest(path),
  now: () => Date.now(),
  setTimeout: (callback, ms) => window.setTimeout(callback, ms),
  clearTimeout: (id) => {
    window.clearTimeout(id);
  },
};

/**
 * TerminalConnection runs one terminal session in the browser: a WebSocket
 * to /api/agents/{id}/terminal carrying the shell's bytes (binary) and
 * control messages (text). If the connection drops without the shell
 * ending, it reconnects and resumes the same shell for up to 30 seconds.
 */
export class TerminalConnection {
  private socket: WebSocket | undefined;
  private sessionId: string | undefined;
  private droppedAt: number | undefined;
  private retryTimer: number | undefined;
  private size: TerminalSize;
  private stopped = false;
  private readonly encoder = new TextEncoder();

  constructor(
    private readonly agentId: string,
    size: TerminalSize,
    private readonly callbacks: TerminalCallbacks,
    private readonly deps: TerminalDeps = defaultDeps,
  ) {
    this.size = size;
  }

  /**
   * start checks the server will open a terminal, then connects. A refusal
   * (a second factor needed, the machine offline, ...) is thrown as the
   * ApiError, so the caller can ask for the second factor and try again.
   */
  async start(): Promise<void> {
    this.callbacks.onState({ kind: 'connecting' });
    await this.deps.preflight(this.path());
    if (!this.stopped) {
      this.connect();
    }
  }

  /** send types data into the shell. */
  send(data: string) {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(this.encoder.encode(data));
    }
  }

  /** resize tells the shell the terminal's new size. */
  resize(size: TerminalSize) {
    this.size = size;
    this.sendControl({ type: 'resize', cols: size.cols, rows: size.rows });
  }

  /** close ends the shell: the user closed the terminal. */
  close() {
    this.sendControl({ type: 'close' });
    this.stop();
  }

  /**
   * detach leaves the page without ending the shell, so it can be resumed
   * for 30 seconds (navigating away by accident shouldn't lose work).
   */
  detach() {
    this.stop();
  }

  private stop() {
    this.stopped = true;
    if (this.retryTimer !== undefined) {
      this.deps.clearTimeout(this.retryTimer);
    }
    this.socket?.close();
  }

  private path(): string {
    const query = new URLSearchParams({
      cols: String(this.size.cols),
      rows: String(this.size.rows),
    });
    if (this.sessionId !== undefined) {
      query.set('resume', this.sessionId);
    }
    return `api/agents/${encodeURIComponent(this.agentId)}/terminal?${query.toString()}`;
  }

  private connect() {
    const url = new URL(apiURL(this.path()));
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
    const socket = this.deps.createSocket(url.toString());
    socket.binaryType = 'arraybuffer';
    this.socket = socket;
    let ended = false;
    socket.addEventListener('message', (event: MessageEvent<ArrayBuffer | string>) => {
      if (typeof event.data === 'string') {
        ended = this.handleStatus(event.data) || ended;
      } else {
        this.callbacks.onOutput(new Uint8Array(event.data));
      }
    });
    socket.addEventListener('close', (event) => {
      if (this.stopped || ended || this.socket !== socket) {
        return;
      }
      if (this.sessionId === undefined) {
        // It never opened: the reason, if any, is the agent's refusal.
        this.callbacks.onState({
          kind: 'ended',
          reason: event.reason || 'The terminal couldn’t be opened.',
        });
        return;
      }
      this.droppedAt ??= this.deps.now();
      this.scheduleRetry(1000);
    });
  }

  /** handleStatus applies a status message; it returns true when the shell ended. */
  private handleStatus(text: string): boolean {
    let status: ServerStatus;
    try {
      status = JSON.parse(text) as ServerStatus;
    } catch {
      return false;
    }
    if (status.type === 'opened') {
      this.sessionId = status.sessionId;
      this.droppedAt = undefined;
      this.callbacks.onState({ kind: 'open', resumed: status.resumed === true });
      return false;
    }
    this.callbacks.onState({ kind: 'ended', reason: status.reason ?? 'The session ended.' });
    return true;
  }

  private scheduleRetry(delayMs: number) {
    const left = resumeWindowMs - (this.deps.now() - (this.droppedAt ?? this.deps.now()));
    if (left <= 0) {
      this.callbacks.onState({
        kind: 'ended',
        reason: 'The connection was lost and couldn’t be restored.',
      });
      return;
    }
    this.callbacks.onState({ kind: 'reconnecting', secondsLeft: Math.ceil(left / 1000) });
    this.retryTimer = this.deps.setTimeout(() => {
      void this.retry(Math.min(delayMs * 2, 5000));
    }, delayMs);
  }

  private async retry(nextDelayMs: number) {
    try {
      await this.deps.preflight(this.path());
    } catch (error) {
      if (this.stopped) {
        return;
      }
      if (isApiError(error, 'not_found')) {
        this.callbacks.onState({ kind: 'ended', reason: error.message });
        return;
      }
      if (isApiError(error) && refusals.has(error.code)) {
        this.callbacks.onState({ kind: 'failed', error });
        return;
      }
      this.scheduleRetry(nextDelayMs); // offline, or the network is still down
      return;
    }
    if (!this.stopped) {
      this.connect();
    }
  }

  private sendControl(control: { type: 'resize' | 'close'; cols?: number; rows?: number }) {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify(control));
    }
  }
}
