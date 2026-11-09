// SPDX-License-Identifier: Apache-2.0
import { Dialog } from '@base-ui/react/dialog';
import { useState } from 'react';
import { useNavigate } from 'react-router';

import { useRunScript } from '../../api/scripts';
import type { Script, ScriptTarget } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import confirmStyles from '../../components/ConfirmDialog/ConfirmDialog.module.css';
import { FormError } from '../../components/FormError/FormError';
import dialogStyles from '../../components/StepUpDialog/StepUpDialog.module.css';
import { TargetPicker } from './TargetPicker';

interface RunDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  script: Script;
}

/** RunDialog chooses where to run a script, runs it, and opens its run. */
export function RunDialog({ open, onOpenChange, script }: RunDialogProps) {
  const navigate = useNavigate();
  const run = useRunScript();
  const [target, setTarget] = useState<ScriptTarget>({ kind: 'agent', value: '' });
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className={dialogStyles.backdrop} />
        <Dialog.Popup className={dialogStyles.popup}>
          <Dialog.Title className={dialogStyles.title}>Run {script.name}</Dialog.Title>
          <TargetPicker target={target} onChange={setTarget} />
          <FormError error={run.error} />
          <div className={confirmStyles.actions}>
            <Dialog.Close className={dialogStyles.close}>Cancel</Dialog.Close>
            <Button
              disabled={target.value === ''}
              isBusy={run.isPending}
              onClick={() => {
                run.mutate(
                  { id: script.id, target },
                  {
                    onSuccess: ({ batchId }) => {
                      onOpenChange(false);
                      void navigate(`/scripts/runs/${String(batchId)}`);
                    },
                  },
                );
              }}
            >
              Run
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
