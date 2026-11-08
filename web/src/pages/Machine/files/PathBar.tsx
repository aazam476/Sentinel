// SPDX-License-Identifier: Apache-2.0
import { useState, type SubmitEvent } from 'react';

import { Button } from '../../../components/Button/Button';
import systemStyles from '../SystemTab.module.css';
import styles from './Files.module.css';
import { crumbs } from './paths';

interface PathBarProps {
  path: string;
  parent: string;
  canChange: boolean;
  onGo: (path: string) => void;
  onNewFolder: () => void;
}

/** PathBar shows where the file manager is, and goes elsewhere. */
export function PathBar({ path, parent, canChange, onGo, onNewFolder }: PathBarProps) {
  const [typed, setTyped] = useState(path);
  const handleSubmit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    onGo(typed.trim());
  };
  return (
    <div className={styles.pathBar}>
      <nav aria-label="Folder path" className={styles.crumbs}>
        <button
          type="button"
          className={systemStyles.linkButton}
          onClick={() => {
            onGo('');
          }}
        >
          Start
        </button>
        {crumbs(path).map((crumb) => (
          <span key={crumb.path} className={styles.crumb}>
            <span aria-hidden="true">›</span>
            <button
              type="button"
              className={systemStyles.linkButton}
              aria-current={crumb.path === path ? 'location' : undefined}
              onClick={() => {
                onGo(crumb.path);
              }}
            >
              {crumb.name}
            </button>
          </span>
        ))}
      </nav>
      <form className={systemStyles.toolbar} onSubmit={handleSubmit}>
        <input
          className={systemStyles.search}
          aria-label="Go to a folder"
          placeholder="/srv/media or C:\Users"
          value={typed}
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          onChange={(event) => {
            setTyped(event.target.value);
          }}
        />
        <Button type="submit" variant="secondary">
          Go
        </Button>
        {parent !== '' && (
          <Button
            variant="secondary"
            onClick={() => {
              onGo(parent);
            }}
          >
            Up
          </Button>
        )}
        {canChange && (
          <Button variant="secondary" onClick={onNewFolder}>
            New folder
          </Button>
        )}
      </form>
    </div>
  );
}
