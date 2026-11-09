// SPDX-License-Identifier: Apache-2.0
import { vi } from 'vitest';

import { recoverFromPreloadErrors } from './preloadRecovery';

function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
  };
}

describe('recovering from a page that failed to load', () => {
  it('reloads once, then leaves a second failure to the error screen', () => {
    const target = new EventTarget();
    const reload = vi.fn();
    recoverFromPreloadErrors(target, { storage: memoryStorage(), reload });
    const first = new Event('vite:preloadError', { cancelable: true });
    target.dispatchEvent(first);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(first.defaultPrevented).toBe(true);
    const second = new Event('vite:preloadError', { cancelable: true });
    target.dispatchEvent(second);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(second.defaultPrevented).toBe(false);
  });

  it('can recover again after a page loaded fine', () => {
    const target = new EventTarget();
    const reload = vi.fn();
    recoverFromPreloadErrors(target, { storage: memoryStorage(), reload });
    target.dispatchEvent(new Event('vite:preloadError', { cancelable: true }));
    target.dispatchEvent(new Event('load'));
    target.dispatchEvent(new Event('vite:preloadError', { cancelable: true }));
    expect(reload).toHaveBeenCalledTimes(2);
  });

  it('never reloads without storage to remember it by', () => {
    const target = new EventTarget();
    const reload = vi.fn();
    const broken = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => undefined,
      removeItem: () => undefined,
    };
    recoverFromPreloadErrors(target, { storage: broken, reload });
    target.dispatchEvent(new Event('vite:preloadError', { cancelable: true }));
    expect(reload).not.toHaveBeenCalled();
  });
});
