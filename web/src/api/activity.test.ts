// SPDX-License-Identifier: Apache-2.0
import { http, HttpResponse } from 'msw';

import { apiPath, server } from '../test/server';
import { activityTracker } from './activity';
import { apiRequest } from './client';

describe('SEC-06 activity', () => {
  it('counts a key press, click, touch or scroll, and the page loading, for a minute', () => {
    let now = 1_000_000;
    const target = new EventTarget();
    const tracker = activityTracker(() => now);
    const stop = tracker.watch(target);
    expect(tracker.isActive()).toBe(true); // just loaded
    now += 61_000;
    expect(tracker.isActive()).toBe(false);
    for (const type of ['keydown', 'pointerdown', 'touchstart', 'wheel']) {
      target.dispatchEvent(new Event(type));
      expect(tracker.isActive()).toBe(true);
      now += 61_000;
      expect(tracker.isActive()).toBe(false);
    }
    stop();
    target.dispatchEvent(new Event('keydown'));
    expect(tracker.isActive()).toBe(false);
  });

  it('marks only requests made while someone is using the page', async () => {
    const seen: (string | null)[] = [];
    server.use(
      http.get(apiPath('thing'), ({ request }) => {
        seen.push(request.headers.get('X-Sentinel-Activity'));
        return HttpResponse.json({});
      }),
    );
    await apiRequest('api/thing', { active: () => true });
    await apiRequest('api/thing', { active: () => false });
    expect(seen).toEqual(['1', null]);
  });
});
