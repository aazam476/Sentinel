// SPDX-License-Identifier: Apache-2.0
import type { Mutation } from '@tanstack/react-query';

import { isApiError } from './client';

type Listener = () => void;

/**
 * stepUpRequests tells the app shell when an action was refused for want
 * of a recent second factor (SEC-07), so one dialog serves every action
 * that doesn't handle the refusal itself.
 */
const listeners = new Set<Listener>();

/** askForStepUp opens the app's step-up dialog. */
export function askForStepUp(): void {
  for (const listener of listeners) {
    listener();
  }
}

/** onStepUpAsked listens for askForStepUp, and returns how to stop. */
export function onStepUpAsked(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * stepUpOnError is the mutation cache's error handler: a mutation whose
 * meta has stepUp opens the dialog when the server asks for a step-up.
 */
export function stepUpOnError(
  error: unknown,
  _variables: unknown,
  _context: unknown,
  mutation: Mutation<unknown, unknown>,
): void {
  if (mutation.meta?.stepUp === true && isApiError(error, 'step_up_required')) {
    askForStepUp();
  }
}
