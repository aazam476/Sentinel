// SPDX-License-Identifier: Apache-2.0
import { useQueryClient } from '@tanstack/react-query';
import { useRef, useState, type ChangeEvent } from 'react';

import { isApiError } from '../../../api/client';
import { cancelUpload, fileKeys, uploadFile } from '../../../api/files';
import { Button } from '../../../components/Button/Button';
import { messageFor } from '../../../components/FormError/messageFor';
import { formatBytes, formatPercent } from '../../../formatting/formatting';
import systemStyles from '../SystemTab.module.css';
import styles from './Files.module.css';
import { joinPath } from './paths';

type UploadState = 'sending' | 'done' | 'failed' | 'exists' | 'canceled';

interface Upload {
  key: number;
  file: File;
  path: string;
  sent: number;
  state: UploadState;
  error?: string;
  controller: AbortController;
}

/**
 * Uploads sends files into the folder, a piece at a time, showing each
 * one's progress. A file that already exists isn't replaced unless asked.
 */
export function Uploads({
  agentId,
  folder,
}: {
  agentId: string;
  folder: string;
  existing: string[];
}) {
  const queryClient = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const [uploads, setUploads] = useState<Upload[]>([]);
  const nextKey = useRef(0);

  const update = (key: number, change: Partial<Upload>) => {
    setUploads((current) =>
      current.map((upload) => (upload.key === key ? { ...upload, ...change } : upload)),
    );
  };
  const send = (upload: Upload, overwrite: boolean) => {
    const controller = new AbortController();
    update(upload.key, { state: 'sending', error: undefined, controller, sent: 0 });
    uploadFile(agentId, upload.path, upload.file, {
      overwrite,
      signal: controller.signal,
      onProgress: (sent) => {
        update(upload.key, { sent });
      },
    })
      .then(() => {
        update(upload.key, { state: 'done', sent: upload.file.size });
        void queryClient.invalidateQueries({ queryKey: fileKeys.all(agentId) });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        update(
          upload.key,
          isApiError(error, 'conflict') && !overwrite
            ? { state: 'exists' }
            : { state: 'failed', error: messageFor(error) },
        );
      });
  };
  const handleFiles = (event: ChangeEvent<HTMLInputElement>) => {
    const chosen = [...(event.target.files ?? [])].map((file): Upload => ({
      key: (nextKey.current += 1),
      file,
      path: joinPath(folder, file.name),
      sent: 0,
      state: 'sending',
      controller: new AbortController(),
    }));
    event.target.value = '';
    setUploads((current) => [...chosen, ...current]);
    for (const upload of chosen) {
      send(upload, false);
    }
  };
  const cancel = (upload: Upload) => {
    upload.controller.abort();
    update(upload.key, { state: 'canceled' });
    void cancelUpload(agentId, upload.path, upload.file.size).catch(() => undefined);
  };

  return (
    <div className={styles.uploads}>
      <input
        ref={input}
        type="file"
        multiple
        hidden
        onChange={handleFiles}
        aria-label="Files to upload"
      />
      <div className={systemStyles.toolbar}>
        <Button variant="secondary" onClick={() => input.current?.click()}>
          Upload files
        </Button>
        {uploads.some((upload) => upload.state !== 'sending') && (
          <button
            type="button"
            className={systemStyles.linkButton}
            onClick={() => {
              setUploads((current) => current.filter((upload) => upload.state === 'sending'));
            }}
          >
            Clear finished
          </button>
        )}
      </div>
      {uploads.length > 0 && (
        <ul className={styles.uploadList} aria-label="Uploads">
          {uploads.map((upload) => (
            <UploadRow
              key={upload.key}
              upload={upload}
              onCancel={() => {
                cancel(upload);
              }}
              onRetry={(overwrite) => {
                send(upload, overwrite);
              }}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

interface UploadRowProps {
  upload: Upload;
  onCancel: () => void;
  onRetry: (overwrite: boolean) => void;
}

function UploadRow({ upload, onCancel, onRetry }: UploadRowProps) {
  const percent = upload.file.size === 0 ? 100 : (upload.sent / upload.file.size) * 100;
  const status: Record<UploadState, string> = {
    sending: `${formatBytes(upload.sent)} of ${formatBytes(upload.file.size)} · ${formatPercent(percent)}`,
    done: `Uploaded · ${formatBytes(upload.file.size)}`,
    failed: upload.error ?? 'The upload failed.',
    exists: 'A file with this name is already there.',
    canceled: 'Canceled',
  };
  return (
    <li className={styles.upload} aria-label={`Upload ${upload.file.name}`}>
      <span className={styles.name} title={upload.path}>
        {upload.file.name}
      </span>
      <progress
        className={styles.progress}
        max={100}
        value={percent}
        aria-label={`${upload.file.name} progress`}
      />
      <span
        className={upload.state === 'failed' ? styles.problem : systemStyles.muted}
        role="status"
      >
        {status[upload.state]}
      </span>
      <span className={styles.uploadActions}>
        {upload.state === 'sending' && (
          <button type="button" className={systemStyles.linkButton} onClick={onCancel}>
            Cancel
          </button>
        )}
        {upload.state === 'failed' && (
          <button
            type="button"
            className={systemStyles.linkButton}
            onClick={() => {
              onRetry(false);
            }}
          >
            Resume
          </button>
        )}
        {upload.state === 'exists' && (
          <button
            type="button"
            className={systemStyles.linkButton}
            onClick={() => {
              onRetry(true);
            }}
          >
            Replace it
          </button>
        )}
      </span>
    </li>
  );
}
