// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { toast } from 'sonner';

import { useSaveStack, useStack, useStackAction, type StackAction } from '../../../api/docker';
import { Button } from '../../../components/Button/Button';
import { CodeEditor } from '../../../components/CodeEditor/CodeEditor';
import { ConfirmDialog } from '../../../components/ConfirmDialog/ConfirmDialog';
import { Field } from '../../../components/Field/Field';
import { FormError } from '../../../components/FormError/FormError';
import { messageFor } from '../../../components/FormError/messageFor';
import { useMachine } from '../machineContext';
import styles from '../SystemTab.module.css';
import { DockerError } from './dockerError';

const actionCopy: Record<StackAction, { label: string; busy: string; done: string }> = {
  up: { label: 'Deploy', busy: 'Deploying', done: 'Deployed' },
  down: { label: 'Stop', busy: 'Stopping', done: 'Stopped' },
  pull: { label: 'Pull and redeploy', busy: 'Pulling and redeploying', done: 'Redeployed' },
};

/**
 * StackEditor edits a stack's compose file and .env, or creates a stack
 * (at stacks/new), and deploys, stops or updates it.
 */
export function StackEditor() {
  const { machine } = useMachine();
  const { stack = 'new' } = useParams();
  const isNew = stack === 'new';
  const existing = useStack(machine.id, stack, !isNew);
  if (!isNew && existing.isPending) {
    return <p className={styles.muted}>Loading the stack…</p>;
  }
  if (!isNew && existing.data === undefined) {
    return <DockerError error={existing.error} />;
  }
  return (
    <StackForm
      key={stack}
      agentId={machine.id}
      initialName={isNew ? '' : stack}
      initialCompose={existing.data?.compose ?? ''}
      initialEnv={existing.data?.env ?? ''}
      isNew={isNew}
      isEditable={isNew || existing.data?.editable === true}
      isManaged={isNew || existing.data?.managed === true}
      services={existing.data?.services ?? []}
    />
  );
}

interface StackFormProps {
  agentId: string;
  initialName: string;
  initialCompose: string;
  initialEnv: string;
  isNew: boolean;
  isEditable: boolean;
  isManaged: boolean;
  services: { service: string; container: string; state: string; status: string; ports: string }[];
}

function StackForm(props: StackFormProps) {
  const { agentId, isNew, isEditable, isManaged } = props;
  const navigate = useNavigate();
  const save = useSaveStack(agentId);
  const action = useStackAction(agentId);
  const [name, setName] = useState(props.initialName);
  const [compose, setCompose] = useState(props.initialCompose);
  const [env, setEnv] = useState(props.initialEnv);
  const [confirmStop, setConfirmStop] = useState(false);

  // The toast follows the request itself, not this component: saving a
  // new stack moves to its own page, and the deploy outlives this form.
  const run = (verb: StackAction) => {
    toast.promise(action.mutateAsync({ name, action: verb }), {
      loading: `${actionCopy[verb].busy} ${name}…`,
      success: `${actionCopy[verb].done} ${name}`,
      error: (error: unknown) => messageFor(error),
    });
  };
  const submit = (validateOnly: boolean, deploy: boolean) => {
    save.mutate(
      { name, compose, env, validateOnly },
      {
        onSuccess: () => {
          if (validateOnly) {
            toast.success('The compose file is valid');
            return;
          }
          toast.success(`Saved ${name}`);
          if (deploy) {
            run('up');
          }
          if (isNew) {
            void navigate(`../${encodeURIComponent(name)}`, { relative: 'path' });
          }
        },
      },
    );
  };
  const busy = save.isPending || action.isPending;
  return (
    <section className={styles.tab} aria-label={isNew ? 'New stack' : `Stack ${name}`}>
      <Link to=".." className={styles.muted}>
        ← Stacks
      </Link>
      {isNew ? (
        <Field
          label="Stack name"
          value={name}
          onChange={(event) => {
            setName(event.target.value.toLowerCase());
          }}
          hint="Lowercase letters, digits, - and _."
          autoCapitalize="none"
          autoCorrect="off"
          maxLength={63}
        />
      ) : (
        <h2 className={styles.name}>{name}</h2>
      )}
      {!isEditable && (
        <p className={styles.muted}>
          This stack was started outside Sentinel and the agent can’t read its compose file, so it
          can be controlled but not edited here.
        </p>
      )}
      {isEditable && (
        <>
          <CodeEditor label="compose.yaml" language="yaml" value={compose} onChange={setCompose} />
          {isManaged && (
            <>
              <p className={styles.muted}>
                .env: values the compose file uses as ${'{NAME}'}. Readable by root only.
              </p>
              <CodeEditor label=".env" language="plain" value={env} onChange={setEnv} />
            </>
          )}
          <div className={styles.toolbar}>
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => {
                submit(true, false);
              }}
            >
              Validate
            </Button>
            <Button
              variant="secondary"
              disabled={busy || name === ''}
              onClick={() => {
                submit(false, false);
              }}
            >
              Save
            </Button>
            <Button
              disabled={busy || name === ''}
              onClick={() => {
                submit(false, true);
              }}
            >
              Save and deploy
            </Button>
          </div>
          <FormError error={save.error} />
        </>
      )}
      {!isNew && (
        <>
          <div className={styles.toolbar}>
            {!isEditable && (
              <Button
                disabled={busy}
                onClick={() => {
                  run('up');
                }}
              >
                Deploy
              </Button>
            )}
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => {
                run('pull');
              }}
            >
              Pull and redeploy
            </Button>
            <Button
              variant="danger"
              disabled={busy}
              onClick={() => {
                setConfirmStop(true);
              }}
            >
              Stop
            </Button>
          </div>
          <ul className={styles.list} aria-label="Containers of the stack">
            {props.services.length === 0 && <li className={styles.row}>Not running.</li>}
            {props.services.map((service) => (
              <li key={service.container} className={styles.row}>
                <span className={styles.rowMain}>
                  <span className={styles.name}>{service.service}</span>
                  <span className={styles.meta}>
                    <span>{service.status}</span>
                    {service.ports !== '' && <span>{service.ports}</span>}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
      <ConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        title={`Stop ${name}?`}
        description="Its containers and networks are removed; its volumes and files are kept, so it can be deployed again."
        confirmLabel="Stop"
        onConfirm={() => {
          run('down');
        }}
      />
    </section>
  );
}
