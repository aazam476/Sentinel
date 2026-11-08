// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { toast } from 'sonner';

import { isApiError } from '../../../api/client';
import { useSaveText, useTextFile } from '../../../api/files';
import type { FileTextPayload } from '../../../api/protocol.gen';
import { Button } from '../../../components/Button/Button';
import { CodeEditor } from '../../../components/CodeEditor/CodeEditor';
import { FormError } from '../../../components/FormError/FormError';
import { messageFor } from '../../../components/FormError/messageFor';
import systemStyles from '../SystemTab.module.css';
import styles from './Files.module.css';

interface TextEditorProps {
  agentId: string;
  path: string;
  onClose: () => void;
}

/** TextEditor opens a text file of up to 1 MB for editing (§9.7). */
export function TextEditor({ agentId, path, onClose }: TextEditorProps) {
  const text = useTextFile(agentId, path);
  if (text.isPending) {
    return <p className={systemStyles.muted}>Opening {path}…</p>;
  }
  if (text.data === undefined) {
    return (
      <section className={styles.tab} aria-label={`Editing ${path}`}>
        <FormError error={text.error} />
        <Button variant="secondary" onClick={onClose}>
          Back to the folder
        </Button>
      </section>
    );
  }
  return (
    <EditorForm key={text.data.modified} agentId={agentId} file={text.data} onClose={onClose} />
  );
}

function EditorForm({
  agentId,
  file,
  onClose,
}: {
  agentId: string;
  file: FileTextPayload;
  onClose: () => void;
}) {
  const save = useSaveText(agentId);
  const [content, setContent] = useState(file.content);
  const [modified, setModified] = useState(file.modified);
  const changed = content !== file.content;
  const handleSave = () => {
    save.mutate(
      { path: file.path, content, modified },
      {
        onSuccess: (entry) => {
          setModified(entry.modified);
          toast.success(`Saved ${entry.name}`);
        },
      },
    );
  };
  return (
    <section className={styles.tab} aria-label={`Editing ${file.path}`}>
      <div className={styles.editorHeader}>
        <span className={styles.path} title={file.path}>
          {file.path}
        </span>
        <div className={systemStyles.toolbar}>
          <Button onClick={handleSave} isBusy={save.isPending}>
            Save
          </Button>
          <Button variant="secondary" onClick={onClose}>
            {changed ? 'Close without saving' : 'Close'}
          </Button>
        </div>
      </div>
      {isApiError(save.error, 'conflict') ? (
        <p className={styles.problem} role="alert">
          {messageFor(save.error)} Copy your changes, then close and open the file again.
        </p>
      ) : (
        <FormError error={save.error} />
      )}
      <CodeEditor label={file.path} language="plain" value={content} onChange={setContent} />
    </section>
  );
}
