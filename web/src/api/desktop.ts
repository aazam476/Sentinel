// SPDX-License-Identifier: Apache-2.0
import { apiRequest, apiURL } from './client';
import { FrameEnd, FrameScreen, FrameTile, type ICEServer } from './protocol.gen';

/**
 * A remote desktop session (§9.3), outside React: the screen is drawn
 * straight onto a canvas as updates arrive, never through component state.
 *
 * The browser asks the server for a session on a WebSocket, then offers
 * the agent a WebRTC connection through it. If the data channel opens, the
 * screen and input go over it, directly. If it doesn't within a few
 * seconds (no route, a tunnel that carries no UDP), or the user asked to,
 * the agent sends the screen over the WebSocket instead, through the
 * server: slower, but it always works.
 */

/** How long WebRTC gets to connect before the session falls back. */
export const webRTCTimeoutMs = 8_000;

export type DesktopTransport = 'direct' | 'relayed';

export type DesktopState =
  | { kind: 'connecting' }
  | { kind: 'open'; transport: DesktopTransport; source: string; width: number; height: number }
  | { kind: 'ended'; reason: string }
  | { kind: 'failed'; error: unknown };

export interface DesktopCallbacks {
  onState: (state: DesktopState) => void;
  /** onResize gives the screen's size in pixels, before its first frame. */
  onResize: (width: number, height: number) => void;
  /** onFrame draws one screen's worth of tiles. */
  onFrame: (tiles: DecodedTile[]) => void;
}

export interface DecodedTile {
  x: number;
  y: number;
  image: ImageBitmap;
}

export interface DesktopInput {
  kind: 'mousemove' | 'mousedown' | 'mouseup' | 'wheel' | 'keydown' | 'keyup' | 'ctrlaltdel';
  x?: number;
  y?: number;
  button?: number;
  deltaX?: number;
  deltaY?: number;
  code?: string;
  key?: string;
}

export interface DesktopOptions {
  viewOnly: boolean;
  /** relayOnly skips WebRTC and sends everything through the server. */
  relayOnly: boolean;
}

/** DesktopDeps are the browser features a session uses; tests replace them. */
export interface DesktopDeps {
  preflight: (path: string) => Promise<unknown>;
  createSocket: (url: string) => WebSocket;
  createPeer: (iceServers: RTCIceServer[]) => RTCPeerConnection;
  decodeImage: (jpeg: Uint8Array) => Promise<ImageBitmap>;
  setTimeout: (callback: () => void, ms: number) => number;
  clearTimeout: (id: number) => void;
}

const defaultDeps: DesktopDeps = {
  preflight: (path) => apiRequest(path),
  createSocket: (url) => new WebSocket(url),
  createPeer: (iceServers) => new RTCPeerConnection({ iceServers }),
  decodeImage: (jpeg) => createImageBitmap(new Blob([jpeg as BlobPart], { type: 'image/jpeg' })),
  setTimeout: (callback, ms) => window.setTimeout(callback, ms),
  clearTimeout: (id) => {
    window.clearTimeout(id);
  },
};

/** machineDesktop is a machine's desktop endpoint. */
export function machineDesktop(agentId: string): string {
  return `api/agents/${encodeURIComponent(agentId)}/desktop`;
}

/**
 * screenPoint maps a point on the canvas as shown (CSS pixels, from its
 * top left) to the machine's screen pixel under it: the pixel whose square
 * holds the point, so a point anywhere inside pixel 400 is pixel 400.
 */
export function screenPoint(
  x: number,
  y: number,
  shown: { width: number; height: number },
  screen: { width: number; height: number },
): { x: number; y: number } {
  if (shown.width === 0 || shown.height === 0) {
    return { x: 0, y: 0 };
  }
  const clamp = (value: number, max: number) =>
    Math.min(Math.max(Math.floor(value), 0), Math.max(max - 1, 0));
  return {
    x: clamp((x * screen.width) / shown.width, screen.width),
    y: clamp((y * screen.height) / shown.height, screen.height),
  };
}

interface ControlMessage {
  type: string;
  sdp?: string;
  candidate?: RTCIceCandidateInit;
  message?: string;
  source?: string;
  width?: number;
  height?: number;
  /** iceServers come with "ready": the server's relay, if it has one. */
  iceServers?: ICEServer[];
}

export class DesktopConnection {
  private socket: WebSocket | undefined;
  private peer: RTCPeerConnection | undefined;
  private input: RTCDataChannel | undefined;
  private transport: DesktopTransport | undefined;
  private fallbackTimer: number | undefined;
  private source = '';
  private iceServers: RTCIceServer[] = [];
  private width = 0;
  private height = 0;
  private pending: Promise<DecodedTile>[] = [];
  private stopped = false;

  constructor(
    private readonly endpoint: string,
    private readonly options: DesktopOptions,
    private readonly callbacks: DesktopCallbacks,
    private readonly deps: DesktopDeps = defaultDeps,
  ) {}

  /**
   * start checks the server will open a session, then connects. A refusal
   * (a second factor needed, the machine offline, ...) is thrown as the
   * ApiError, so the caller can ask for the second factor and try again.
   */
  async start(): Promise<void> {
    this.callbacks.onState({ kind: 'connecting' });
    await this.deps.preflight(this.path());
    if (this.stopped) {
      return;
    }
    const url = new URL(apiURL(this.path()));
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
    const socket = this.deps.createSocket(url.toString());
    socket.binaryType = 'arraybuffer';
    socket.onmessage = (event: MessageEvent<string | ArrayBuffer>) => {
      if (typeof event.data === 'string') {
        void this.handleControl(event.data);
      } else {
        this.handleUpdate(new Uint8Array(event.data), true);
      }
    };
    socket.onclose = (event) => {
      this.end(event.reason || 'The session ended.');
    };
    this.socket = socket;
  }

  /** send passes a mouse or keyboard event to the machine. */
  send(input: DesktopInput) {
    if (this.options.viewOnly) {
      return;
    }
    const message = JSON.stringify({ type: 'input', input });
    if (this.transport === 'direct' && this.input?.readyState === 'open') {
      this.input.send(message);
    } else {
      this.sendControl(message);
    }
  }

  /** setQuality changes the picture's JPEG quality, 10 to 95. */
  setQuality(quality: number) {
    this.sendControl(JSON.stringify({ type: 'quality', quality }));
  }

  /** close ends the session. */
  close() {
    this.stop();
    this.callbacks.onState({ kind: 'ended', reason: 'You closed the session.' });
  }

  private path(): string {
    return this.options.viewOnly ? `${this.endpoint}?viewOnly=true` : this.endpoint;
  }

  private sendControl(message: string) {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(message);
    }
  }

  private async handleControl(text: string) {
    let message: ControlMessage;
    try {
      message = JSON.parse(text) as ControlMessage;
    } catch {
      return;
    }
    switch (message.type) {
      case 'ready':
        this.source = message.source ?? '';
        this.iceServers = (message.iceServers ?? []).map((server) => ({
          urls: server.urls,
          username: server.username,
          credential: server.credential,
        }));
        if (this.options.relayOnly) {
          this.fallBack();
        } else {
          await this.offer();
        }
        break;
      case 'answer':
        await this.peer?.setRemoteDescription({ type: 'answer', sdp: message.sdp });
        break;
      case 'candidate':
        if (message.candidate !== undefined) {
          await this.peer?.addIceCandidate(message.candidate).catch(() => undefined);
        }
        break;
      case 'error':
        this.fallBack();
        break;
    }
  }

  /** offer opens a WebRTC connection to the agent, falling back if it can't. */
  private async offer() {
    let peer: RTCPeerConnection;
    try {
      peer = this.deps.createPeer(this.iceServers);
    } catch {
      this.fallBack();
      return;
    }
    this.peer = peer;
    const frames = peer.createDataChannel('frames');
    frames.binaryType = 'arraybuffer';
    frames.onopen = () => {
      if (this.transport === undefined) {
        this.deps.clearTimeout(this.fallbackTimer ?? 0);
        this.transport = 'direct';
        this.announce();
      }
    };
    frames.onmessage = (event: MessageEvent<ArrayBuffer>) => {
      this.handleUpdate(new Uint8Array(event.data), false);
    };
    this.input = peer.createDataChannel('input');
    // Candidates found before the offer is sent wait for it: the agent
    // can't use a candidate for a connection it hasn't heard of.
    let early: string[] | undefined = [];
    peer.onicecandidate = (event) => {
      if (event.candidate !== null) {
        const message = JSON.stringify({ type: 'candidate', candidate: event.candidate.toJSON() });
        if (early === undefined) {
          this.sendControl(message);
        } else {
          early.push(message);
        }
      }
    };
    this.fallbackTimer = this.deps.setTimeout(() => {
      this.fallBack();
    }, webRTCTimeoutMs);
    const description = await peer.createOffer();
    await peer.setLocalDescription(description);
    this.sendControl(JSON.stringify({ type: 'offer', sdp: description.sdp }));
    early.forEach((message) => {
      this.sendControl(message);
    });
    early = undefined;
  }

  /** fallBack has the agent send the screen through the server. */
  private fallBack() {
    if (this.transport !== undefined) {
      return;
    }
    this.deps.clearTimeout(this.fallbackTimer ?? 0);
    this.peer?.close();
    this.peer = undefined;
    this.transport = 'relayed';
    this.sendControl(JSON.stringify({ type: 'fallback' }));
    this.announce();
  }

  private announce() {
    if (this.transport !== undefined) {
      this.callbacks.onState({
        kind: 'open',
        transport: this.transport,
        source: this.source,
        width: this.width,
        height: this.height,
      });
    }
  }

  /**
   * handleUpdate decodes a screen update. Tiles are decoded as they come
   * and drawn together at the frame's end, so a frame never shows half
   * drawn; a relayed frame is then acknowledged, so the next comes.
   */
  private handleUpdate(data: Uint8Array, relayed: boolean) {
    const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
    switch (data[0]) {
      case FrameScreen:
        this.width = view.getUint16(1);
        this.height = view.getUint16(3);
        this.callbacks.onResize(this.width, this.height);
        this.announce();
        break;
      case FrameTile: {
        const x = view.getUint16(1);
        const y = view.getUint16(3);
        this.pending.push(
          this.deps.decodeImage(data.subarray(9)).then((image) => ({ x, y, image })),
        );
        break;
      }
      case FrameEnd: {
        const tiles = this.pending;
        this.pending = [];
        void Promise.all(tiles).then(
          (decoded) => {
            this.callbacks.onFrame(decoded);
            if (relayed) {
              this.sendControl(JSON.stringify({ type: 'ack' }));
            }
          },
          () => {
            // A tile that can't be decoded: ask for a whole new screen.
            this.sendControl(JSON.stringify({ type: 'keyframe' }));
            if (relayed) {
              this.sendControl(JSON.stringify({ type: 'ack' }));
            }
          },
        );
        break;
      }
    }
  }

  private end(reason: string) {
    if (!this.stopped) {
      this.stop();
      this.callbacks.onState({ kind: 'ended', reason });
    }
  }

  private stop() {
    this.stopped = true;
    this.deps.clearTimeout(this.fallbackTimer ?? 0);
    this.peer?.close();
    this.socket?.close();
  }
}
