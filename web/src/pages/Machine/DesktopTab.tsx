// SPDX-License-Identifier: Apache-2.0
import { useEffect, useRef, useState } from 'react';

import { isApiError } from '../../api/client';
import {
  DesktopConnection,
  machineDesktop,
  screenPoint,
  type DesktopInput,
  type DesktopOptions,
  type DesktopState,
} from '../../api/desktop';
import type { SessionResponse } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { FormError } from '../../components/FormError/FormError';
import { StepUpDialog } from '../../components/StepUpDialog/StepUpDialog';
import styles from './DesktopTab.module.css';
import { useMachine } from './machineContext';

/** The picture's JPEG quality to start with, as the agent's default. */
const defaultQuality = 70;

/** DesktopTab shows the machine's screen and controls it (§9.3). */
export function DesktopTab() {
  const { session, machine } = useMachine();
  const [options, setOptions] = useState<DesktopOptions>({ viewOnly: false, relayOnly: false });
  // A new key starts a new session: after the last one ended, when the
  // options change, or once the user has confirmed their second factor.
  const [attempt, setAttempt] = useState(0);
  if (!machine.online) {
    return (
      <p className={styles.notice}>The machine is offline. Remote desktop opens when it’s back.</p>
    );
  }
  if (!machine.desktopAvailable) {
    return (
      <p className={styles.notice}>
        {machine.desktopReason || 'Remote desktop isn’t available on this machine.'}
      </p>
    );
  }
  return (
    <DesktopView
      key={`${String(attempt)}-${String(options.viewOnly)}-${String(options.relayOnly)}`}
      endpoint={machineDesktop(machine.id)}
      session={session}
      windows={machine.os === 'windows'}
      options={options}
      onOptionsChange={setOptions}
      onRestart={() => {
        setAttempt((n) => n + 1);
      }}
    />
  );
}

interface DesktopViewProps {
  endpoint: string;
  session: SessionResponse;
  /** windows offers Ctrl+Alt+Del, which only Windows needs. */
  windows: boolean;
  options: DesktopOptions;
  onOptionsChange: (options: DesktopOptions) => void;
  onRestart: () => void;
}

/** DesktopView is one desktop session, drawn onto a canvas. */
export function DesktopView({
  endpoint,
  session,
  windows,
  options,
  onOptionsChange,
  onRestart,
}: DesktopViewProps) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const frame = useRef<HTMLDivElement>(null);
  const connection = useRef<DesktopConnection>(null);
  const [state, setState] = useState<DesktopState>({ kind: 'connecting' });
  const [quality, setQuality] = useState(defaultQuality);

  // An effect because the connection and the canvas live outside React:
  // frames are drawn as they arrive, and leaving the tab ends the session.
  useEffect(() => {
    const element = canvas.current;
    const context = element?.getContext('2d');
    if (element === null || context === null || context === undefined) {
      return undefined;
    }
    const desktop = new DesktopConnection(endpoint, options, {
      onState: setState,
      onResize: (width, height) => {
        element.width = width;
        element.height = height;
      },
      onFrame: (tiles) => {
        for (const tile of tiles) {
          context.drawImage(tile.image, tile.x, tile.y);
          tile.image.close();
        }
      },
    });
    connection.current = desktop;
    desktop.start().catch((error: unknown) => {
      setState({ kind: 'failed', error });
    });
    return () => {
      desktop.close();
      connection.current = null;
    };
    // options are part of the component's key, so they never change here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [endpoint]);

  const send = (input: DesktopInput) => {
    connection.current?.send(input);
  };
  const needsStepUp = state.kind === 'failed' && isApiError(state.error, 'step_up_required');
  const isOpen = state.kind === 'open';
  const isLive = isOpen || state.kind === 'connecting';
  return (
    <section className={styles.desktop} aria-label="Remote desktop">
      <div className={styles.bar}>
        <p className={styles.status} role="status">
          {statusText(state)}
        </p>
        <div className={styles.actions}>
          {isOpen && windows && !options.viewOnly && (
            <Button
              variant="secondary"
              onClick={() => {
                send({ kind: 'ctrlaltdel' });
              }}
            >
              Ctrl+Alt+Del
            </Button>
          )}
          {/* Phones' browsers (Safari on iPhone) can't show an element full screen. */}
          {isOpen && document.fullscreenEnabled && (
            <Button
              variant="secondary"
              onClick={() => {
                void frame.current?.requestFullscreen().catch(() => undefined);
              }}
            >
              Full screen
            </Button>
          )}
          {isLive ? (
            <Button
              variant="secondary"
              onClick={() => {
                connection.current?.close();
              }}
            >
              End session
            </Button>
          ) : (
            !needsStepUp && (
              <Button variant="secondary" onClick={onRestart}>
                New session
              </Button>
            )
          )}
        </div>
      </div>
      <div className={styles.settings}>
        <label className={styles.check}>
          <input
            type="checkbox"
            checked={options.viewOnly}
            onChange={(event) => {
              onOptionsChange({ ...options, viewOnly: event.target.checked });
            }}
          />
          View only
        </label>
        <label className={styles.check}>
          <input
            type="checkbox"
            checked={options.relayOnly}
            onChange={(event) => {
              onOptionsChange({ ...options, relayOnly: event.target.checked });
            }}
          />
          Relay through the server
        </label>
        <label className={styles.quality}>
          Quality
          <input
            type="range"
            min={10}
            max={95}
            step={5}
            value={quality}
            onChange={(event) => {
              const value = Number(event.target.value);
              setQuality(value);
              connection.current?.setQuality(value);
            }}
          />
          <span className={styles.qualityValue}>{quality}</span>
        </label>
      </div>
      {state.kind === 'failed' && !needsStepUp && <FormError error={state.error} />}
      <div ref={frame} className={styles.frame}>
        <Screen canvasRef={canvas} interactive={isOpen && !options.viewOnly} send={send} />
      </div>
      <StepUpDialog
        open={needsStepUp}
        onOpenChange={(open) => {
          if (!open) {
            setState({
              kind: 'ended',
              reason: 'Remote desktop needs a fresh check of your second factor.',
            });
          }
        }}
        secondFactors={session.secondFactors}
        onConfirmed={onRestart}
      />
    </section>
  );
}

interface ScreenProps {
  canvasRef: React.RefObject<HTMLCanvasElement | null>;
  interactive: boolean;
  send: (input: DesktopInput) => void;
}

/** Screen is the canvas, turning the user's mouse and keys into input. */
function Screen({ canvasRef, interactive, send }: ScreenProps) {
  const point = (event: React.PointerEvent | React.WheelEvent) => {
    const element = event.currentTarget as HTMLCanvasElement;
    const rect = element.getBoundingClientRect();
    return screenPoint(event.clientX - rect.left, event.clientY - rect.top, rect, element);
  };
  const key = (kind: 'keydown' | 'keyup') => (event: React.KeyboardEvent) => {
    event.preventDefault();
    send({ kind, code: event.code, key: event.key });
  };
  if (!interactive) {
    return <canvas ref={canvasRef} className={styles.screen} aria-label="Screen" />;
  }
  return (
    <canvas
      ref={canvasRef}
      className={styles.screen}
      aria-label="Screen"
      tabIndex={0}
      onPointerMove={(event) => {
        send({ kind: 'mousemove', ...point(event) });
      }}
      onPointerDown={(event) => {
        // The point first, and a focus that doesn't scroll: a scroll here
        // would move the screen under the pointer before it is read.
        const at = point(event);
        event.currentTarget.focus({ preventScroll: true });
        event.currentTarget.setPointerCapture(event.pointerId);
        send({ kind: 'mousedown', button: event.button, ...at });
      }}
      onPointerUp={(event) => {
        send({ kind: 'mouseup', button: event.button, ...point(event) });
      }}
      onWheel={(event) => {
        send({
          kind: 'wheel',
          deltaX: Math.sign(event.deltaX),
          deltaY: Math.sign(event.deltaY),
          ...point(event),
        });
      }}
      onContextMenu={(event) => {
        event.preventDefault();
      }}
      onKeyDown={key('keydown')}
      onKeyUp={key('keyup')}
    />
  );
}

function statusText(state: DesktopState): string {
  switch (state.kind) {
    case 'connecting':
      return 'Connecting…';
    case 'open': {
      const how = state.transport === 'direct' ? 'Direct (WebRTC)' : 'Through the server';
      return state.source === '' ? how : `${how} · ${state.source}`;
    }
    case 'ended':
      return state.reason;
    case 'failed':
      return 'Remote desktop couldn’t be opened.';
  }
}
