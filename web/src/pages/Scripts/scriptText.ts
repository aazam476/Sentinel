// SPDX-License-Identifier: Apache-2.0
import cronstrue from 'cronstrue';

import { formatNumber } from '../../formatting/formatting';

export const languageNames: Record<string, string> = {
  bash: 'Bash',
  sh: 'sh',
  powershell: 'PowerShell',
  python: 'Python',
};

export const systemNames: Record<string, string> = {
  any: 'Any system',
  linux: 'Linux',
  darwin: 'macOS',
  windows: 'Windows',
};

export const stateNames: Record<string, string> = {
  running: 'Running',
  succeeded: 'Succeeded',
  failed: 'Failed',
  timed_out: 'Timed out',
  offline: 'Offline',
  skipped: 'Skipped',
  canceled: 'Canceled',
};

/** stateTone picks the dot beside a run's state. */
export function stateTone(state: string): 'good' | 'bad' | 'warning' | 'neutral' {
  switch (state) {
    case 'succeeded':
      return 'good';
    case 'failed':
    case 'timed_out':
      return 'bad';
    case 'offline':
    case 'canceled':
      return 'warning';
  }
  return 'neutral';
}

/**
 * describeCron says a cron schedule in words, "At 03:00 AM, Monday through
 * Friday", or undefined when it isn't one cronstrue understands.
 */
export function describeCron(cron: string): string | undefined {
  try {
    return cronstrue.toString(cron, { use24HourTimeFormat: true, verbose: false });
  } catch {
    return undefined;
  }
}

/** describeTarget says where a script runs, in words. */
export function describeTarget(
  target: { kind: string; value: string },
  names: Map<string, string>,
): string {
  switch (target.kind) {
    case 'tag':
      return `Machines tagged ${target.value}`;
    case 'agent':
      return names.get(target.value) ?? 'A machine that is gone';
    default: {
      const ids = target.value.split(',').filter((id) => id !== '');
      return ids.length === 1
        ? (names.get(ids[0] ?? '') ?? '1 machine')
        : `${formatNumber(ids.length)} machines`;
    }
  }
}
