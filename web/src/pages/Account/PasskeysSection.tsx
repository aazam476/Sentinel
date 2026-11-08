// SPDX-License-Identifier: Apache-2.0
import { useState, type SubmitEvent } from 'react';
import { toast } from 'sonner';

import { isApiError } from '../../api/client';
import {
  PasskeyCancelledError,
  useAddPasskey,
  usePasskeys,
  useRemovePasskey,
} from '../../api/passkeys';
import type { PasskeySummary, SessionResponse } from '../../api/types.gen';
import { passkeysSupported } from '../../api/webauthn';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import { StepUpDialog } from '../../components/StepUpDialog/StepUpDialog';
import { useStepUpRetry } from '../../components/StepUpDialog/useStepUpRetry';
import { formatDateTime } from '../../formatting/formatting';
import styles from './AccountPage.module.css';

interface PasskeysSectionProps {
  session: SessionResponse;
}

/** PasskeysSection lists the user's passkeys and adds or removes them. */
export function PasskeysSection({ session }: PasskeysSectionProps) {
  const passkeys = usePasskeys();
  const addPasskey = useAddPasskey();
  const removePasskey = useRemovePasskey();
  const stepUp = useStepUpRetry();
  const [name, setName] = useState('');
  const [toRemove, setToRemove] = useState<PasskeySummary | null>(null);

  const add = () => {
    addPasskey.mutate(name.trim(), {
      onSuccess: () => {
        setName('');
        toast.success('Passkey added');
      },
      onError: (error) => stepUp.handleError(error, add),
    });
  };

  const remove = (passkey: PasskeySummary) => {
    removePasskey.mutate(passkey.id, {
      onSuccess: () => toast.success(`Removed “${passkey.name}”`),
      onError: (error) =>
        stepUp.handleError(error, () => {
          remove(passkey);
        }),
    });
  };

  const handleAdd = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    add();
  };

  // A closed passkey prompt is a choice, and a step-up request is handled by
  // the dialog; neither is shown as an error.
  const visibleError = (error: unknown) =>
    error instanceof PasskeyCancelledError || isApiError(error, 'step_up_required') ? null : error;

  return (
    <section className={styles.section} aria-labelledby="passkeys-title">
      <h2 id="passkeys-title" className={styles.sectionTitle}>
        Passkeys
      </h2>
      <p className={styles.sectionText}>
        Sign in with your device’s fingerprint, face or PIN instead of a code.
      </p>
      <PasskeyList
        passkeys={passkeys.data?.passkeys}
        isLoading={passkeys.isPending}
        onRemove={setToRemove}
      />
      <FormError error={passkeys.error ?? visibleError(removePasskey.error)} />
      {passkeysSupported() && (
        <form className={styles.addForm} onSubmit={handleAdd}>
          <Field
            label="New passkey name"
            value={name}
            onChange={(event) => {
              setName(event.target.value);
            }}
            maxLength={64}
            placeholder="Work laptop"
          />
          <Button type="submit" variant="secondary" isBusy={addPasskey.isPending}>
            Add passkey
          </Button>
        </form>
      )}
      <FormError error={visibleError(addPasskey.error)} />
      <ConfirmDialog
        open={toRemove !== null}
        onOpenChange={(open) => {
          if (!open) setToRemove(null);
        }}
        title="Remove this passkey?"
        description={`“${toRemove?.name ?? ''}” will no longer sign you in.`}
        confirmLabel="Remove"
        onConfirm={() => {
          if (toRemove !== null) remove(toRemove);
        }}
      />
      <StepUpDialog
        open={stepUp.isOpen}
        onOpenChange={stepUp.setOpen}
        secondFactors={session.secondFactors}
        onConfirmed={stepUp.confirmed}
      />
    </section>
  );
}

interface PasskeyListProps {
  passkeys: PasskeySummary[] | undefined;
  isLoading: boolean;
  onRemove: (passkey: PasskeySummary) => void;
}

function PasskeyList({ passkeys, isLoading, onRemove }: PasskeyListProps) {
  if (isLoading) {
    return <p className={styles.sectionText}>Loading…</p>;
  }
  if (passkeys === undefined || passkeys.length === 0) {
    return <p className={styles.empty}>No passkeys yet.</p>;
  }
  return (
    <ul className={styles.list}>
      {passkeys.map((passkey) => (
        <li key={passkey.id} className={styles.item}>
          <div className={styles.itemMain}>
            <span className={styles.itemName}>{passkey.name}</span>
            <span className={styles.itemMeta}>
              Added {formatDateTime(passkey.createdAt)}
              {passkey.lastUsedAt !== null && ` · Last used ${formatDateTime(passkey.lastUsedAt)}`}
            </span>
          </div>
          <Button
            variant="danger"
            aria-label={`Remove passkey ${passkey.name}`}
            onClick={() => {
              onRemove(passkey);
            }}
          >
            Remove
          </Button>
        </li>
      ))}
    </ul>
  );
}
