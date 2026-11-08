// SPDX-License-Identifier: Apache-2.0
import { AlertDialog } from '@base-ui/react/alert-dialog';
import type { ReactNode } from 'react';

import dialogStyles from '../StepUpDialog/StepUpDialog.module.css';
import { Button } from '../Button/Button';
import styles from './ConfirmDialog.module.css';

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  onConfirm: () => void;
}

/** ConfirmDialog asks before an action that can't be undone. */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
}: ConfirmDialogProps) {
  return (
    <AlertDialog.Root open={open} onOpenChange={onOpenChange}>
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className={dialogStyles.backdrop} />
        <AlertDialog.Popup className={dialogStyles.popup}>
          <AlertDialog.Title className={dialogStyles.title}>{title}</AlertDialog.Title>
          <AlertDialog.Description className={dialogStyles.description}>
            {description}
          </AlertDialog.Description>
          <div className={styles.actions}>
            <AlertDialog.Close className={dialogStyles.close}>Cancel</AlertDialog.Close>
            <Button
              variant="danger"
              onClick={() => {
                onOpenChange(false);
                onConfirm();
              }}
            >
              {confirmLabel}
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
