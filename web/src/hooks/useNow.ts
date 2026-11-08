// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from 'react';

/**
 * useNow is the current time, refreshed every interval milliseconds, for
 * labels like "last seen 5 minutes ago" that should stay true while the
 * page is open. Reading the clock during render would make rendering
 * impure.
 */
export function useNow(interval = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  // An effect because the clock is outside React: the timer must stop when
  // the component unmounts.
  useEffect(() => {
    const timer = window.setInterval(() => {
      setNow(Date.now());
    }, interval);
    return () => {
      window.clearInterval(timer);
    };
  }, [interval]);
  return now;
}
