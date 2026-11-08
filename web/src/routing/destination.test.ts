// SPDX-License-Identifier: Apache-2.0
import { adminSession, enrollingSession, pendingSession } from '../test/fixtures/normal/fixtures';
import { areaFor, homeOf, routerBasename } from './destination';

describe('areaFor', () => {
  it.each([
    ['sends a fresh install to setup, whatever the session', true, adminSession, 'setup'],
    ['sends a browser without a session to sign-in', false, null, 'login'],
    [
      'sends a password-only session to the second-factor step',
      false,
      pendingSession,
      'second-factor',
    ],
    ['sends a user without a second factor to enrollment', false, enrollingSession, 'enroll'],
    ['lets a signed-in session into the app', false, adminSession, 'app'],
  ] as const)('%s', (_, setupRequired, session, expected) => {
    expect(areaFor(setupRequired, session)).toBe(expected);
  });

  it('gives every area a home page', () => {
    expect(Object.values(homeOf)).toEqual([
      '/setup',
      '/login',
      '/login/verify',
      '/login/enroll',
      '/',
    ]);
  });
});

describe('routerBasename', () => {
  it.each([
    ['https://sentinel.example.com/', ''],
    ['https://example.com/sentinel/', '/sentinel'],
    ['https://example.com/tools/sentinel/', '/tools/sentinel'],
    ['https://example.com/no-slash', '/no-slash'],
  ])('turns %s into %j', (baseURI, expected) => {
    expect(routerBasename(baseURI)).toBe(expected);
  });
});
