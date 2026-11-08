// SPDX-License-Identifier: Apache-2.0
import { ApiError } from '../../api/client';
import { messageFor } from './messageFor';

function apiError(code: string, message: string, retryAfterSeconds?: number) {
  return new ApiError(400, { error: { code, message, retryAfterSeconds } });
}

describe('messageFor', () => {
  it.each([
    [
      'writes the server message as a sentence',
      apiError('invalid_code', 'the code is incorrect'),
      'The code is incorrect.',
    ],
    ['keeps a message that already ends a sentence', apiError('x', 'Done!'), 'Done!'],
    [
      'says how long to wait in seconds',
      apiError('too_many_attempts', 'slow down', 1),
      'Too many attempts. Try again in 1 second.',
    ],
    [
      'uses plural seconds',
      apiError('too_many_attempts', 'slow down', 8),
      'Too many attempts. Try again in 8 seconds.',
    ],
    [
      'rounds long waits up to minutes',
      apiError('too_many_attempts', 'slow down', 61),
      'Too many attempts. Try again in 2 minutes.',
    ],
    [
      'says one minute',
      apiError('too_many_attempts', 'slow down', 60),
      'Too many attempts. Try again in 1 minute.',
    ],
    [
      'explains a network failure',
      new TypeError('Failed to fetch'),
      'Sentinel could not be reached. Check your connection and try again.',
    ],
    ['falls back for anything else', new Error('boom'), 'Something went wrong. Try again.'],
  ])('%s', (_, error, expected) => {
    expect(messageFor(error)).toBe(expected);
  });
});
