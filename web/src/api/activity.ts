// SPDX-License-Identifier: Apache-2.0

/** activeFor is how long after a key press, click, touch or scroll the page counts as in use. */
const activeFor = 60_000;

/** The events that mean someone is at the page. */
const activityEvents = ['keydown', 'pointerdown', 'touchstart', 'wheel'] as const;

/**
 * activityTracker notices when someone uses the page. Requests made while
 * nobody has for a minute (polling, refreshes after server events) don't
 * keep the session from timing out (SEC-06).
 */
export function activityTracker(now: () => number = Date.now) {
  let last = now();
  const note = () => {
    last = now();
  };
  return {
    isActive: () => now() - last < activeFor,
    /** watch listens on target (the window) and returns how to stop. */
    watch: (target: EventTarget) => {
      for (const type of activityEvents) {
        target.addEventListener(type, note, { passive: true, capture: true });
      }
      return () => {
        for (const type of activityEvents) {
          target.removeEventListener(type, note, { capture: true });
        }
      };
    },
  };
}

/** pageActivity is this page's tracker; main.tsx starts it watching the window. */
export const pageActivity = activityTracker();
