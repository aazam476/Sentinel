// SPDX-License-Identifier: Apache-2.0
import { isApiError } from '../../api/client';

/** messageFor turns any thrown value into a sentence for people. */
export function messageFor(error: unknown): string {
  if (isApiError(error, 'too_many_attempts') && error.retryAfterSeconds !== undefined) {
    return `Too many attempts. Try again in ${formatWait(error.retryAfterSeconds)}.`;
  }
  if (isApiError(error)) {
    return sentence(error.message);
  }
  if (error instanceof TypeError) {
    return 'Sentinel could not be reached. Check your connection and try again.';
  }
  return 'Something went wrong. Try again.';
}

function formatWait(seconds: number): string {
  if (seconds < 60) {
    return seconds === 1 ? '1 second' : `${String(seconds)} seconds`;
  }
  const minutes = Math.ceil(seconds / 60);
  return minutes === 1 ? '1 minute' : `${String(minutes)} minutes`;
}

/** sentence capitalizes the server's message and ends it with a full stop. */
function sentence(message: string): string {
  const trimmed = message.trim();
  const capitalized = trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(capitalized) ? capitalized : `${capitalized}.`;
}
