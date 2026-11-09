// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { useNavigate, useParams } from 'react-router';
import { toast } from 'sonner';

import { useDeleteScript, useSaveScript, useScripts } from '../../api/scripts';
import type { Script } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { CodeEditor } from '../../components/CodeEditor/CodeEditor';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { Field } from '../../components/Field/Field';
import { FormError } from '../../components/FormError/FormError';
import systemStyles from '../Machine/SystemTab.module.css';
import { RunDialog } from './RunDialog';
import styles from './Scripts.module.css';
import { languageNames, systemNames } from './scriptText';

const blank: Script = {
  id: 0,
  name: '',
  description: '',
  language: 'bash',
  body: '#!/usr/bin/env bash\nset -euo pipefail\n\n',
  targetOS: 'any',
  timeoutSeconds: 600,
  createdAt: 0,
  updatedAt: 0,
};

/** ScriptEditor writes a new script, or edits, runs or deletes one. */
export function ScriptEditor() {
  const { scriptId = 'new' } = useParams();
  const scripts = useScripts();
  if (scriptId === 'new') {
    return <ScriptForm key="new" script={blank} />;
  }
  if (scripts.isPending) {
    return <p className={styles.muted}>Loading the script…</p>;
  }
  const script = scripts.data?.scripts.find((candidate) => String(candidate.id) === scriptId);
  if (script === undefined) {
    return scripts.isError ? (
      <FormError error={scripts.error} />
    ) : (
      <p className={styles.muted}>There is no such script.</p>
    );
  }
  return <ScriptForm key={script.id} script={script} />;
}

function ScriptForm({ script }: { script: Script }) {
  const navigate = useNavigate();
  const save = useSaveScript();
  const remove = useDeleteScript();
  const [draft, setDraft] = useState(script);
  const [running, setRunning] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const isNew = script.id === 0;
  const change = (fields: Partial<Script>) => {
    setDraft((current) => ({ ...current, ...fields }));
  };
  const handleSave = () => {
    save.mutate(
      {
        id: isNew ? undefined : script.id,
        name: draft.name,
        description: draft.description,
        language: draft.language,
        body: draft.body,
        targetOS: draft.targetOS,
        timeoutSeconds: draft.timeoutSeconds,
      },
      {
        onSuccess: (saved) => {
          toast.success(`Saved ${saved.name}`);
          if (isNew) {
            void navigate(`/scripts/${String(saved.id)}`);
          }
        },
      },
    );
  };
  return (
    <section className={styles.section} aria-label={isNew ? 'New script' : `Script ${script.name}`}>
      <Field
        label="Name"
        value={draft.name}
        maxLength={100}
        onChange={(event) => {
          change({ name: event.target.value });
        }}
      />
      <Field
        label="Description"
        value={draft.description}
        maxLength={1000}
        onChange={(event) => {
          change({ description: event.target.value });
        }}
      />
      <div className={styles.settings}>
        <label className={styles.setting}>
          <span>Language</span>
          <select
            className={systemStyles.select}
            value={draft.language}
            onChange={(event) => {
              change({ language: event.target.value });
            }}
          >
            {Object.entries(languageNames).map(([value, name]) => (
              <option key={value} value={value}>
                {name}
              </option>
            ))}
          </select>
        </label>
        <label className={styles.setting}>
          <span>Runs on</span>
          <select
            className={systemStyles.select}
            value={draft.targetOS}
            onChange={(event) => {
              change({ targetOS: event.target.value });
            }}
          >
            {Object.entries(systemNames).map(([value, name]) => (
              <option key={value} value={value}>
                {name}
              </option>
            ))}
          </select>
        </label>
        <label className={styles.setting}>
          <span>Time limit (minutes)</span>
          <input
            className={systemStyles.search}
            type="number"
            min={1}
            max={1440}
            value={Math.round(draft.timeoutSeconds / 60)}
            onChange={(event) => {
              change({ timeoutSeconds: Math.max(1, Number(event.target.value)) * 60 });
            }}
          />
        </label>
      </div>
      <CodeEditor
        label="Script"
        language="plain"
        value={draft.body}
        onChange={(body) => {
          change({ body });
        }}
      />
      <FormError error={save.error} />
      <div className={systemStyles.toolbar}>
        <Button onClick={handleSave} isBusy={save.isPending}>
          Save
        </Button>
        {!isNew && (
          <>
            <Button
              variant="secondary"
              onClick={() => {
                setRunning(true);
              }}
            >
              Run…
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                setDeleting(true);
              }}
            >
              Delete
            </Button>
          </>
        )}
      </div>
      {!isNew && <RunDialog open={running} onOpenChange={setRunning} script={script} />}
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`Delete ${script.name}?`}
        description="Its schedules are deleted too. Its past runs and their output are kept."
        confirmLabel="Delete"
        onConfirm={() => {
          remove.mutate(script.id, {
            onSuccess: () => {
              toast.success(`Deleted ${script.name}`);
              void navigate('/scripts');
            },
          });
        }}
      />
    </section>
  );
}
