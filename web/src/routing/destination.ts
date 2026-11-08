// SPDX-License-Identifier: Apache-2.0
import type { SessionResponse } from '../api/types.gen';

/** Area is a group of pages that only makes sense in one sign-in state. */
export type Area = 'setup' | 'login' | 'second-factor' | 'enroll' | 'app';

/** homeOf is the page each area starts at. */
export const homeOf: Record<Area, string> = {
  setup: '/setup',
  login: '/login',
  'second-factor': '/login/verify',
  enroll: '/login/enroll',
  app: '/',
};

/**
 * areaFor decides where the browser belongs: setup until the first admin
 * exists, then sign-in until both steps are done, then the app.
 */
export function areaFor(setupRequired: boolean, session: SessionResponse | null): Area {
  if (setupRequired) {
    return 'setup';
  }
  if (session === null) {
    return 'login';
  }
  switch (session.state) {
    case 'second_factor':
      return 'second-factor';
    case 'enroll_second_factor':
      return 'enroll';
    default:
      return 'app';
  }
}

/**
 * routerBasename is the base path the server put in the page's <base>
 * element, without the trailing slash, so links work under any base path.
 */
export function routerBasename(baseURI: string): string {
  const path = new URL(baseURI).pathname;
  return path.endsWith('/') ? path.slice(0, -1) : path;
}
