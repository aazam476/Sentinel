// SPDX-License-Identifier: Apache-2.0
import { useSyncExternalStore } from 'react';

/**
 * useMediaQuery reports whether a CSS media query matches, and re-renders
 * when that changes. Canvas charts need it to follow the color scheme and
 * reduced-motion settings that stylesheets get for free.
 */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      if (typeof window.matchMedia !== 'function') {
        return () => undefined;
      }
      const list = window.matchMedia(query);
      list.addEventListener('change', onChange);
      return () => {
        list.removeEventListener('change', onChange);
      };
    },
    () => typeof window.matchMedia === 'function' && window.matchMedia(query).matches,
  );
}
