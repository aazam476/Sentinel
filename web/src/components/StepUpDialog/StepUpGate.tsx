// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from 'react';
import { toast } from 'sonner';

import { onStepUpAsked } from '../../api/stepUp';
import type { SecondFactors } from '../../api/types.gen';
import { StepUpDialog } from './StepUpDialog';

/**
 * StepUpGate is the app's one step-up dialog for actions that don't ask
 * for the check themselves (SEC-07): it opens when one is refused for want
 * of a recent second factor, and asks the person to do it again after.
 */
export function StepUpGate({ secondFactors }: { secondFactors: SecondFactors }) {
  const [open, setOpen] = useState(false);
  useEffect(
    () =>
      onStepUpAsked(() => {
        setOpen(true);
      }),
    [],
  );
  return (
    <StepUpDialog
      open={open}
      onOpenChange={setOpen}
      secondFactors={secondFactors}
      onConfirmed={() => {
        setOpen(false);
        toast.success('Confirmed. Now do that again.');
      }}
    />
  );
}
