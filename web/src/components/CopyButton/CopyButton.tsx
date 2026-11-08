// SPDX-License-Identifier: Apache-2.0
import { toast } from 'sonner';

import { Button } from '../Button/Button';

interface CopyButtonProps {
  text: string;
  /** label names what is copied, for screen readers and the toast. */
  label: string;
}

/**
 * CopyButton copies text to the clipboard and confirms with a toast. Pages
 * served over plain HTTP have no clipboard API; then it says to select the
 * text by hand, which always works because the text is on screen.
 */
export function CopyButton({ text, label }: CopyButtonProps) {
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(`Copied the ${label}`);
    } catch {
      toast.error(`Couldn’t copy automatically. Select the ${label} and copy it.`);
    }
  };

  return (
    <Button
      variant="secondary"
      aria-label={`Copy the ${label}`}
      onClick={() => {
        void handleCopy();
      }}
    >
      Copy
    </Button>
  );
}
