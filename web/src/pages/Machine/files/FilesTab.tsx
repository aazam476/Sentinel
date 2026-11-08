// SPDX-License-Identifier: Apache-2.0
import { forwardRef, useState, type HTMLAttributes } from 'react';
import { useSearchParams } from 'react-router';
import { Virtuoso, type Components } from 'react-virtuoso';
import { toast } from 'sonner';

import { useDeleteFile, useFileList, useMkdir, useRename } from '../../../api/files';
import type { FileEntry } from '../../../api/protocol.gen';
import { ConfirmDialog } from '../../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../../components/FormError/FormError';
import { messageFor } from '../../../components/FormError/messageFor';
import { NameDialog } from '../../../components/NameDialog/NameDialog';
import { countOf, formatNumber } from '../../../formatting/formatting';
import { allows, useMachine } from '../machineContext';
import systemStyles from '../SystemTab.module.css';
import { FileRow } from './FileRow';
import styles from './Files.module.css';
import { joinPath } from './paths';
import { PathBar } from './PathBar';
import { TextEditor } from './TextEditor';
import { Uploads } from './Uploads';

/**
 * FilesTab is the machine's file manager (§9.7): browse, download, upload,
 * create, rename, delete, and edit small text files. The folder and the
 * file being edited are in the address, so a reload or a shared link
 * opens the same place.
 */
export function FilesTab() {
  const { machine } = useMachine();
  const [params, setParams] = useSearchParams();
  const path = params.get('path') ?? '';
  const editing = params.get('edit');
  if (!allows(machine, 'files')) {
    return (
      <p className={systemStyles.muted}>
        The file manager is disabled by this machine’s local policy.
      </p>
    );
  }
  if (!machine.online) {
    return (
      <p className={systemStyles.muted}>The machine is offline. Its files show when it’s back.</p>
    );
  }
  const open = (next: { path?: string; edit?: string }) => {
    const search = new URLSearchParams();
    if (next.path !== undefined && next.path !== '') {
      search.set('path', next.path);
    }
    if (next.edit !== undefined) {
      search.set('edit', next.edit);
    }
    setParams(search);
  };
  if (editing !== null) {
    return (
      <TextEditor
        agentId={machine.id}
        path={editing}
        onClose={() => {
          open({ path });
        }}
      />
    );
  }
  return <FolderView key={path} agentId={machine.id} path={path} onOpen={open} />;
}

interface FolderViewProps {
  agentId: string;
  path: string;
  onOpen: (next: { path?: string; edit?: string }) => void;
}

function FolderView({ agentId, path, onOpen }: FolderViewProps) {
  const listing = useFileList(agentId, path);
  const mkdir = useMkdir(agentId);
  const rename = useRename(agentId);
  const remove = useDeleteFile(agentId);
  const [creating, setCreating] = useState(false);
  const [renaming, setRenaming] = useState<FileEntry>();
  const [deleting, setDeleting] = useState<FileEntry>();
  const report = (done: string) => ({
    onSuccess: () => toast.success(done),
    onError: (error: unknown) => toast.error(messageFor(error)),
  });
  const entries = listing.data?.entries ?? [];
  return (
    <section className={styles.tab} aria-label="Files">
      <PathBar
        path={listing.data?.path ?? path}
        parent={listing.data?.parent ?? ''}
        onGo={(next) => {
          onOpen({ path: next });
        }}
        canChange={path !== ''}
        onNewFolder={() => {
          setCreating(true);
        }}
      />
      {path !== '' && (
        <Uploads agentId={agentId} folder={path} existing={entries.map((entry) => entry.name)} />
      )}
      {listing.isPending && <p className={systemStyles.muted}>Loading the folder…</p>}
      {listing.isError && <FormError error={listing.error} />}
      {listing.data !== undefined && (
        <>
          <p className={systemStyles.count}>
            {countOf(entries.length, 'item', 'items')}
            {listing.data.truncated &&
              ` · only the first ${formatNumber(entries.length)} are shown`}
          </p>
          {entries.length === 0 ? (
            <p className={systemStyles.muted}>This folder is empty.</p>
          ) : (
            <Virtuoso
              useWindowScroll
              data={entries}
              computeItemKey={(_, entry) => entry.path}
              components={listParts}
              itemContent={(_, entry) => (
                <FileRow
                  agentId={agentId}
                  entry={entry}
                  isStart={path === ''}
                  onOpen={(next) => {
                    // Editing keeps the folder in the address, to come back to.
                    onOpen(next.edit === undefined ? next : { path, edit: next.edit });
                  }}
                  onRename={setRenaming}
                  onDelete={setDeleting}
                />
              )}
            />
          )}
        </>
      )}
      <NameDialog
        open={creating}
        onOpenChange={setCreating}
        title="New folder"
        label="Folder name"
        initialName=""
        submitLabel="Create"
        onSubmit={(name) => {
          mkdir.mutate(joinPath(path, name), report(`Created ${name}`));
        }}
      />
      <NameDialog
        open={renaming !== undefined}
        onOpenChange={(isOpen) => {
          if (!isOpen) {
            setRenaming(undefined);
          }
        }}
        title={`Rename ${renaming?.name ?? ''}`}
        label="New name"
        initialName={renaming?.name ?? ''}
        submitLabel="Rename"
        onSubmit={(name) => {
          if (renaming !== undefined) {
            rename.mutate(
              { from: renaming.path, to: joinPath(path, name) },
              report(`Renamed to ${name}`),
            );
          }
        }}
      />
      <ConfirmDialog
        open={deleting !== undefined}
        onOpenChange={(isOpen) => {
          if (!isOpen) {
            setDeleting(undefined);
          }
        }}
        title={`Delete ${deleting?.name ?? ''}?`}
        description={
          deleting?.type === 'dir'
            ? 'The folder and everything in it are deleted. This can’t be undone.'
            : 'This can’t be undone.'
        }
        confirmLabel="Delete"
        onConfirm={() => {
          if (deleting !== undefined) {
            remove.mutate(
              { path: deleting.path, recursive: deleting.type === 'dir' },
              report(`Deleted ${deleting.name}`),
            );
          }
        }}
      />
    </section>
  );
}

/**
 * listParts make the virtualized list a real list, so screen readers
 * announce it as one. A folder can hold thousands of entries, and only
 * those on screen are rendered.
 */
const listParts: Components<FileEntry> = {
  List: forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(function FileList(props, ref) {
    return <div {...props} ref={ref} role="list" className={styles.list} />;
  }),
  Item: ({ children, item, ...props }) => (
    <div {...props} role="listitem" aria-label={item.name}>
      {children}
    </div>
  ),
};
