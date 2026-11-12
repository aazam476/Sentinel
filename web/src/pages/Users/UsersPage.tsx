// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { useOutletContext } from 'react-router';
import { toast } from 'sonner';

import type { SessionResponse, UpdateUserRequest, UserResponse } from '../../api/types.gen';
import { useCreateUser, useUpdateUser, useUsers } from '../../api/users';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import { StepUpDialog } from '../../components/StepUpDialog/StepUpDialog';
import { useStepUpRetry } from '../../components/StepUpDialog/useStepUpRetry';
import { formatDateTime } from '../../formatting/formatting';
import alertStyles from '../Alerts/Alerts.module.css';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from '../Scripts/Scripts.module.css';
import { AddUserDialog } from './AddUserDialog';
import { TwoFactorSetting } from './TwoFactorSetting';
import { roles } from './roles';

/**
 * UsersPage lets admins add users, change their roles and disable them
 * (§9.10). Every change asks for a fresh second-factor check if the last
 * one was a while ago (SEC-07).
 */
export function UsersPage() {
  const session = useOutletContext<SessionResponse>();
  const users = useUsers();
  const create = useCreateUser();
  const update = useUpdateUser();
  const stepUp = useStepUpRetry();
  const [isAdding, setIsAdding] = useState(false);
  const [disabling, setDisabling] = useState<UserResponse>();

  if (session.user.role !== 'admin') {
    return (
      <div className={styles.page}>
        <h1 className={styles.title}>Users</h1>
        <p className={styles.muted}>Only admins manage users.</p>
      </div>
    );
  }

  const change = (user: UserResponse, request: UpdateUserRequest, done: string) => {
    update.mutate(
      { id: user.id, ...request },
      {
        onSuccess: () => toast.success(done),
        onError: (error) => {
          if (
            !stepUp.handleError(error, () => {
              change(user, request, done);
            })
          ) {
            toast.error(messageFor(error));
          }
        },
      },
    );
  };

  return (
    <div className={styles.page}>
      <h1 className={styles.title}>Users</h1>
      <TwoFactorSetting
        withoutTwoFactor={
          users.data?.users.filter((user) => !user.twoFactor && !user.disabled).length ?? 0
        }
        onStepUp={stepUp.handleError}
      />
      <section className={styles.section} aria-label="Users">
        <div className={systemStyles.toolbar}>
          <Button
            onClick={() => {
              setIsAdding(true);
            }}
          >
            Add user
          </Button>
        </div>
        {users.isPending && <p className={styles.muted}>Loading users…</p>}
        <FormError error={users.error} />
        {users.data !== undefined && (
          <ul className={systemStyles.list}>
            {users.data.users.map((user) => {
              const isMe = user.id === session.user.id;
              return (
                <li key={user.id} className={styles.row} aria-label={user.username}>
                  <span className={styles.runHeader}>
                    <span className={styles.name}>{user.username}</span>
                    {isMe && <span className={systemStyles.badge}>You</span>}
                    {user.disabled && <span className={systemStyles.badge}>Disabled</span>}
                    {!user.twoFactor && <span className={systemStyles.badge}>No 2FA</span>}
                  </span>
                  <span className={systemStyles.meta}>
                    <span>Added {formatDateTime(user.createdAt)}</span>
                  </span>
                  <span className={alertStyles.actions}>
                    <label className={styles.setting}>
                      <span>Role</span>
                      <select
                        className={systemStyles.select}
                        value={user.role}
                        disabled={isMe}
                        title={isMe ? 'Another admin can change your role.' : undefined}
                        onChange={(event) => {
                          const role = event.target.value;
                          change(user, { role }, `${user.username} is now ${role}`);
                        }}
                      >
                        {roles.map((role) => (
                          <option key={role.value} value={role.value}>
                            {role.label}
                          </option>
                        ))}
                      </select>
                    </label>
                    {!isMe &&
                      (user.disabled ? (
                        <Button
                          variant="secondary"
                          onClick={() => {
                            change(user, { disabled: false }, `${user.username} can sign in again`);
                          }}
                        >
                          Enable
                        </Button>
                      ) : (
                        <Button
                          variant="secondary"
                          onClick={() => {
                            setDisabling(user);
                          }}
                        >
                          Disable
                        </Button>
                      ))}
                  </span>
                </li>
              );
            })}
          </ul>
        )}
      </section>
      {isAdding && (
        <AddUserDialog
          isBusy={create.isPending}
          onClose={() => {
            setIsAdding(false);
          }}
          onAdd={function add(request, failed) {
            create.mutate(request, {
              onSuccess: () => {
                toast.success(`Added ${request.username}`);
                setIsAdding(false);
              },
              onError: (error) => {
                if (
                  !stepUp.handleError(error, () => {
                    add(request, failed);
                  })
                ) {
                  failed(error);
                }
              },
            });
          }}
        />
      )}
      <ConfirmDialog
        open={disabling !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setDisabling(undefined);
          }
        }}
        title={`Disable ${disabling?.username ?? 'this user'}?`}
        description="They are signed out everywhere and can’t sign in until they are enabled again."
        confirmLabel="Disable"
        onConfirm={() => {
          if (disabling !== undefined) {
            change(disabling, { disabled: true }, `Disabled ${disabling.username}`);
          }
        }}
      />
      <StepUpDialog
        open={stepUp.isOpen}
        onOpenChange={stepUp.setOpen}
        secondFactors={session.secondFactors}
        onConfirmed={stepUp.confirmed}
      />
    </div>
  );
}
