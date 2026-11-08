// SPDX-License-Identifier: Apache-2.0
import { yaml } from '@codemirror/lang-yaml';
import { EditorView, basicSetup } from 'codemirror';
import { useEffect, useEffectEvent, useRef } from 'react';

import styles from './CodeEditor.module.css';

interface CodeEditorProps {
  value: string;
  onChange: (value: string) => void;
  /** label names the editor for screen readers. */
  label: string;
  /** language highlights YAML (compose files) or nothing (.env). */
  language: 'yaml' | 'plain';
  readOnly?: boolean;
}

/**
 * CodeEditor is a CodeMirror editor for compose files and .env. It owns
 * its text while mounted: value seeds it, and later changes to value from
 * outside (loading another file) replace the text.
 */
export function CodeEditor({
  value,
  onChange,
  label,
  language,
  readOnly = false,
}: CodeEditorProps) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);
  // The text the editor starts with; later values are applied below.
  const initialValue = useRef(value);
  const emitChange = useEffectEvent((text: string) => {
    onChange(text);
  });

  // An effect because the editor is a DOM widget outside React, created
  // once and destroyed with the component.
  useEffect(() => {
    if (host.current === null) {
      return undefined;
    }
    const editor = new EditorView({
      parent: host.current,
      doc: initialValue.current,
      extensions: [
        basicSetup,
        ...(language === 'yaml' ? [yaml()] : []),
        EditorView.lineWrapping,
        EditorView.editable.of(!readOnly),
        EditorView.contentAttributes.of({ 'aria-label': label }),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            emitChange(update.state.doc.toString());
          }
        }),
      ],
    });
    view.current = editor;
    return () => {
      editor.destroy();
      view.current = null;
    };
  }, [language, readOnly, label]);

  // Text loaded from outside replaces the editor's, unless it is what the
  // editor already holds (the user's own typing coming back).
  useEffect(() => {
    const editor = view.current;
    if (editor !== null && editor.state.doc.toString() !== value) {
      editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: value } });
    }
  }, [value]);

  return <div ref={host} className={styles.editor} />;
}
