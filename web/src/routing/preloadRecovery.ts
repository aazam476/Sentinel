// SPDX-License-Identifier: Apache-2.0

/** reloadedKey marks a reload made to recover, so it happens only once. */
const reloadedKey = 'sentinel:reloaded-after-preload-error';

interface RecoveryOptions {
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | undefined;
  reload: () => void;
}

/**
 * recoverFromPreloadErrors reloads the page once when a page's code or
 * styles fail to load. That happens when the server was upgraded while the
 * page was open (the old files are gone), or when a request failed in
 * transit. Reloading fetches the current files. A second failure in a row
 * is left to the router's error screen, so a broken server can't make the
 * page reload forever.
 */
export function recoverFromPreloadErrors(
  target: EventTarget,
  { storage, reload }: RecoveryOptions,
) {
  target.addEventListener('vite:preloadError', (event) => {
    if (storage === undefined || !markReload(storage)) {
      return; // reloaded already, or no way to stop a reload loop
    }
    event.preventDefault();
    reload();
  });
  // A page that loaded fine clears the mark, so a later failure can recover too.
  target.addEventListener('load', () => {
    try {
      storage?.removeItem(reloadedKey);
    } catch {
      // nothing to clear
    }
  });
}

/** markReload records a recovery reload; false if one was made already. */
function markReload(storage: Pick<Storage, 'getItem' | 'setItem'>): boolean {
  try {
    if (storage.getItem(reloadedKey) !== null) {
      return false;
    }
    storage.setItem(reloadedKey, '1');
    return true;
  } catch {
    return false;
  }
}
