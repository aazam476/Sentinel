// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';

import { isApiError } from '../../api/client';

/**
 * useStepUpRetry handles a sensitive action that the server refused with
 * step_up_required: it opens the step-up dialog and, once the person has
 * confirmed, runs the action again.
 */
export function useStepUpRetry() {
  const [retry, setRetry] = useState<(() => void) | null>(null);

  return {
    isOpen: retry !== null,
    /**
     * handleError opens the dialog when error asks for a step-up, and
     * reports whether it did, so other errors can be shown as usual.
     */
    handleError: (error: unknown, again: () => void): boolean => {
      if (!isApiError(error, 'step_up_required')) {
        return false;
      }
      setRetry(() => again);
      return true;
    },
    setOpen: (open: boolean) => {
      if (!open) {
        setRetry(null);
      }
    },
    confirmed: () => {
      retry?.();
      setRetry(null);
    },
  };
}
