// SPDX-License-Identifier: Apache-2.0
import { Menu } from '@base-ui/react/menu';
import clsx from 'clsx';

import { downloadURL } from '../../../api/files';
import type { FileEntry } from '../../../api/protocol.gen';
import { formatBytes, formatDateTime } from '../../../formatting/formatting';
import systemStyles from '../SystemTab.module.css';
import styles from './Files.module.css';
import { isEditable } from './paths';

interface FileRowProps {
  agentId: string;
  entry: FileEntry;
  /** isStart: the entry is a starting folder, which can't be renamed or deleted. */
  isStart: boolean;
  onOpen: (next: { path?: string; edit?: string }) => void;
  onRename: (entry: FileEntry) => void;
  onDelete: (entry: FileEntry) => void;
}

const icons: Record<string, string> = { dir: '▸', file: '·', symlink: '↪', other: '◦' };

/** FileRow is one file or folder; a folder's name opens it. */
export function FileRow({ agentId, entry, isStart, onOpen, onRename, onDelete }: FileRowProps) {
  const isFolder = entry.type === 'dir';
  return (
    <div className={styles.row}>
      <span className={styles.icon} aria-hidden="true">
        {icons[entry.type] ?? '·'}
      </span>
      <span className={styles.main}>
        {isFolder || entry.type === 'symlink' ? (
          <button
            type="button"
            className={clsx(systemStyles.linkButton, styles.name)}
            title={entry.path}
            onClick={() => {
              onOpen({ path: entry.path });
            }}
          >
            {entry.name}
          </button>
        ) : (
          <span className={styles.name} title={entry.path}>
            {entry.name}
          </span>
        )}
        <span className={systemStyles.meta}>
          {entry.type === 'file' && <span>{formatBytes(entry.sizeBytes)}</span>}
          {entry.type === 'symlink' && entry.target !== undefined && (
            <span className={styles.target} title={entry.target}>
              → {entry.target}
            </span>
          )}
          <span>{formatDateTime(entry.modified)}</span>
          <span className={styles.mode}>{entry.mode}</span>
        </span>
      </span>
      <Menu.Root>
        <Menu.Trigger className={systemStyles.menuTrigger} aria-label={`Actions for ${entry.name}`}>
          ⋯
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner align="end" sideOffset={4}>
            <Menu.Popup className={systemStyles.menu}>
              {isFolder && (
                <Menu.Item
                  className={systemStyles.menuItem}
                  onClick={() => {
                    onOpen({ path: entry.path });
                  }}
                >
                  Open
                </Menu.Item>
              )}
              {entry.type === 'file' && (
                <Menu.Item
                  className={systemStyles.menuItem}
                  render={<a href={downloadURL(agentId, entry.path)} download />}
                >
                  Download
                </Menu.Item>
              )}
              {entry.type === 'file' && isEditable(entry.sizeBytes) && (
                <Menu.Item
                  className={systemStyles.menuItem}
                  onClick={() => {
                    onOpen({ edit: entry.path });
                  }}
                >
                  Edit
                </Menu.Item>
              )}
              {!isStart && (
                <>
                  <Menu.Item
                    className={systemStyles.menuItem}
                    onClick={() => {
                      onRename(entry);
                    }}
                  >
                    Rename
                  </Menu.Item>
                  <Menu.Item
                    className={clsx(systemStyles.menuItem, systemStyles.menuItemDanger)}
                    onClick={() => {
                      onDelete(entry);
                    }}
                  >
                    Delete
                  </Menu.Item>
                </>
              )}
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
    </div>
  );
}
