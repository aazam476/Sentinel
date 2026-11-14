// SPDX-License-Identifier: Apache-2.0
import { Dialog } from '@base-ui/react/dialog';
import { useState } from 'react';
import { useNavigate } from 'react-router';

import { isApiError } from '../../api/client';
import { useRunScript } from '../../api/scripts';
import type { Script, ScriptTarget, SecondFactors } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import confirmStyles from '../../components/ConfirmDialog/ConfirmDialog.module.css';
import { FormError } from '../../components/FormError/FormError';
import { StepUpDialog } from '../../components/StepUpDialog/StepUpDialog';
import dialogStyles from '../../components/StepUpDialog/StepUpDialog.module.css';
import { useStepUpRetry } from '../../components/StepUpDialog/useStepUpRetry';
import { TargetPicker } from './TargetPicker';

interface RunDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  script: Script;
  secondFactors: SecondFactors;
}

/**
 * RunDialog chooses where to run a script, runs it, and opens its run. On
 * more than one machine the server asks to confirm it’s you first, when
 * the last check was a while ago (SEC-07).
 */
export function RunDialog({ open, onOpenChange, script, secondFactors }: RunDialogProps) {
  const navigate = useNavigate();
  const run = useRunScript();
  const stepUp = useStepUpRetry();
  const [target, setTarget] = useState<ScriptTarget>({ kind: 'agent', value: '' });
  const start = () => {
    run.mutate(
      { id: script.id, target },
      {
        onSuccess: ({ batchId }) => {
          onOpenChange(false);
          void navigate(`/scripts/runs/${String(batchId)}`);
        },
        onError: (error) => stepUp.handleError(error, start),
      },
    );
  };
  return (
    <>
      <Dialog.Root open={open} onOpenChange={onOpenChange}>
        <Dialog.Portal>
          <Dialog.Backdrop className={dialogStyles.backdrop} />
          <Dialog.Popup className={dialogStyles.popup}>
            <Dialog.Title className={dialogStyles.title}>Run {script.name}</Dialog.Title>
            <TargetPicker target={target} onChange={setTarget} />
            <FormError error={isApiError(run.error, 'step_up_required') ? null : run.error} />
            <div className={confirmStyles.actions}>
              <Dialog.Close className={dialogStyles.close}>Cancel</Dialog.Close>
              <Button disabled={target.value === ''} isBusy={run.isPending} onClick={start}>
                Run
              </Button>
            </div>
          </Dialog.Popup>
        </Dialog.Portal>
      </Dialog.Root>
      <StepUpDialog
        open={stepUp.isOpen}
        onOpenChange={stepUp.setOpen}
        secondFactors={secondFactors}
        onConfirmed={stepUp.confirmed}
      />
    </>
  );
}
