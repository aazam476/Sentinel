// SPDX-License-Identifier: Apache-2.0
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';
import { useEffect, useRef, useState } from 'react';

import { isApiError } from '../../api/client';
import { machineTerminal, TerminalConnection, type TerminalState } from '../../api/terminal';
import type { SessionResponse } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { FormError } from '../../components/FormError/FormError';
import { StepUpDialog } from '../../components/StepUpDialog/StepUpDialog';
import { defineSentinelStyle } from '../../components/Terminal/sentinelStyle';
import { useMachine } from './machineContext';
import styles from './TerminalTab.module.css';

/** Keys a phone's keyboard lacks, sent as the bytes a terminal expects. */
const touchKeys = [
  { label: 'Esc', data: '\x1b' },
  { label: 'Tab', data: '\t' },
  { label: 'Ctrl-C', data: '\x03' },
  { label: '↑', name: 'Up', data: '\x1b[A' },
  { label: '↓', name: 'Down', data: '\x1b[B' },
  { label: '←', name: 'Left', data: '\x1b[D' },
  { label: '→', name: 'Right', data: '\x1b[C' },
];

/** TerminalTab is a root shell on the machine, in the browser (§9.2). */
export function TerminalTab() {
  const { session, machine } = useMachine();
  // A new key starts a new session: after the last one ended, or once the
  // user has confirmed their second factor.
  const [attempt, setAttempt] = useState(0);
  if (!machine.online) {
    return (
      <p className={styles.notice}>The machine is offline. The terminal opens when it’s back.</p>
    );
  }
  return (
    <TerminalView
      key={attempt}
      endpoint={machineTerminal(machine.id)}
      session={session}
      onRestart={() => {
        setAttempt((n) => n + 1);
      }}
    />
  );
}

interface TerminalViewProps {
  /** endpoint is the terminal's WebSocket path; see api/terminal.ts. */
  endpoint: string;
  session: SessionResponse;
  onRestart: () => void;
}

/** TerminalView is a live terminal on endpoint: a machine's or a container's shell. */
export function TerminalView({ endpoint, session, onRestart }: TerminalViewProps) {
  const container = useRef<HTMLDivElement>(null);
  const terminal = useRef<{ term: Terminal; connection: TerminalConnection }>(null);
  const [state, setState] = useState<TerminalState>({ kind: 'connecting' });

  // An effect because the terminal and its WebSocket live outside React and
  // must be torn down when the tab closes. Leaving the tab detaches rather
  // than ends the shell, so coming back within 30 seconds can resume it.
  useEffect(() => {
    const element = container.current;
    if (element === null) {
      return undefined;
    }
    defineSentinelStyle();
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: getComputedStyle(element).fontFamily,
      fontSize: 14,
      scrollback: 5000,
      theme: { background: '#16171a', foreground: '#e8e9ec', cursor: '#e8e9ec' },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(element);
    fit.fit();
    const connection = new TerminalConnection(
      endpoint,
      { cols: term.cols, rows: term.rows },
      {
        onOutput: (data) => {
          term.write(data);
        },
        onState: setState,
      },
    );
    terminal.current = { term, connection };
    const subscriptions = [
      term.onData((data) => {
        connection.send(data);
      }),
      term.onResize((size) => {
        connection.resize(size);
      }),
    ];
    const observer = new ResizeObserver(() => {
      fit.fit();
    });
    observer.observe(element);
    connection.start().then(
      () => {
        term.focus();
      },
      (error: unknown) => {
        setState({ kind: 'failed', error });
      },
    );
    return () => {
      observer.disconnect();
      subscriptions.forEach((subscription) => {
        subscription.dispose();
      });
      connection.detach();
      term.dispose();
      terminal.current = null;
    };
  }, [endpoint]);

  const needsStepUp = state.kind === 'failed' && isApiError(state.error, 'step_up_required');
  const isLive = state.kind === 'open' || state.kind === 'reconnecting';
  return (
    <section className={styles.terminal} aria-label="Terminal">
      <div className={styles.bar}>
        <p className={styles.status} role="status">
          {statusText(state)}
        </p>
        {isLive ? (
          <Button
            variant="secondary"
            onClick={() => {
              terminal.current?.connection.close();
              setState({ kind: 'ended', reason: 'You closed the session.' });
            }}
          >
            End session
          </Button>
        ) : (
          (state.kind === 'ended' || (state.kind === 'failed' && !needsStepUp)) && (
            <Button variant="secondary" onClick={onRestart}>
              New session
            </Button>
          )
        )}
      </div>
      {state.kind === 'failed' && !needsStepUp && <FormError error={state.error} />}
      <div ref={container} className={styles.screen} />
      {isLive && (
        <div className={styles.touchKeys} aria-label="Extra keys">
          {touchKeys.map((key) => (
            <button
              key={key.label}
              type="button"
              className={styles.touchKey}
              aria-label={key.name ?? key.label}
              // Keeps the focus, and with it the phone's keyboard, on the terminal.
              onPointerDown={(event) => {
                event.preventDefault();
              }}
              onClick={() => {
                terminal.current?.connection.send(key.data);
                terminal.current?.term.focus();
              }}
            >
              {key.label}
            </button>
          ))}
        </div>
      )}
      <StepUpDialog
        open={needsStepUp}
        onOpenChange={(open) => {
          if (!open) {
            setState({
              kind: 'ended',
              reason: 'The terminal needs a fresh check of your second factor.',
            });
          }
        }}
        secondFactors={session.secondFactors}
        onConfirmed={onRestart}
      />
    </section>
  );
}

function statusText(state: TerminalState): string {
  switch (state.kind) {
    case 'connecting':
      return 'Connecting…';
    case 'open':
      return state.resumed ? 'Reconnected to your session' : 'Connected';
    case 'reconnecting':
      return `Connection lost. Reconnecting (${String(state.secondsLeft)} s left)…`;
    case 'ended':
      return state.reason;
    case 'failed':
      return 'The terminal couldn’t be opened.';
  }
}
