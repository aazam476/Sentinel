// SPDX-License-Identifier: Apache-2.0
import { Dialog } from '@base-ui/react/dialog';
import { useState, type SubmitEvent } from 'react';

import { Button } from '../Button/Button';
import styles from '../ConfirmDialog/ConfirmDialog.module.css';
import { Field } from '../Field/Field';
import dialogStyles from '../StepUpDialog/StepUpDialog.module.css';

interface NameDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  label: string;
  initialName: string;
  submitLabel: string;
  onSubmit: (name: string) => void;
}

/**
 * NameDialog asks for one name, such as a new folder's. The name can't be
 * empty or contain a slash: it names one entry, not a path.
 */
export function NameDialog(props: NameDialogProps) {
  return (
    <Dialog.Root open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className={dialogStyles.backdrop} />
        <Dialog.Popup className={dialogStyles.popup}>
          {/* Keyed by the starting name, so each opening starts fresh. */}
          <NameForm key={props.open ? props.initialName : ''} {...props} />
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function NameForm({
  title,
  label,
  initialName,
  submitLabel,
  onSubmit,
  onOpenChange,
}: NameDialogProps) {
  const [name, setName] = useState(initialName);
  const trimmed = name.trim();
  const invalid = trimmed === '' || trimmed === '.' || trimmed === '..' || /[/\\]/.test(trimmed);
  const handleSubmit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!invalid) {
      onOpenChange(false);
      onSubmit(trimmed);
    }
  };
  return (
    <form onSubmit={handleSubmit}>
      <Dialog.Title className={dialogStyles.title}>{title}</Dialog.Title>
      <Field
        label={label}
        value={name}
        onChange={(event) => {
          setName(event.target.value);
        }}
        autoCapitalize="none"
        autoCorrect="off"
        autoFocus
        hint={/[/\\]/.test(trimmed) ? 'A name can’t contain a slash.' : undefined}
      />
      <div className={styles.actions}>
        <Dialog.Close className={dialogStyles.close}>Cancel</Dialog.Close>
        <Button type="submit" disabled={invalid}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}
