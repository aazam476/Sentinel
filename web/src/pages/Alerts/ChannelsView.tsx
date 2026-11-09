// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { toast } from 'sonner';

import { useChannels, useDeleteChannel, useSaveChannel, useTestChannel } from '../../api/alerts';
import type { NotificationChannel } from '../../api/types.gen';
import { Button } from '../../components/Button/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog/ConfirmDialog';
import { FormError } from '../../components/FormError/FormError';
import { messageFor } from '../../components/FormError/messageFor';
import { formatDateTime } from '../../formatting/formatting';
import systemStyles from '../Machine/SystemTab.module.css';
import styles from '../Scripts/Scripts.module.css';
import alertStyles from './Alerts.module.css';
import { channelTypes, labelOf } from './alertText';
import { ChannelDialog } from './ChannelDialog';

/** ChannelsView lists the notification channels, tests and edits them. */
export function ChannelsView() {
  const channels = useChannels();
  const save = useSaveChannel();
  const test = useTestChannel();
  const remove = useDeleteChannel();
  const [editing, setEditing] = useState<NotificationChannel | 'new'>();
  const [deleting, setDeleting] = useState<NotificationChannel>();
  return (
    <section className={styles.section} aria-label="Notification channels">
      <div className={systemStyles.toolbar}>
        <Button
          onClick={() => {
            setEditing('new');
          }}
        >
          New channel
        </Button>
      </div>
      {channels.isPending && <p className={styles.muted}>Loading channels…</p>}
      {channels.isError && <FormError error={channels.error} />}
      {channels.data?.channels.length === 0 && (
        <p className={styles.muted}>
          No channels yet. Add email, a webhook, ntfy or Discord to be told about alerts.
        </p>
      )}
      {channels.data !== undefined && channels.data.channels.length > 0 && (
        <ul className={systemStyles.list}>
          {channels.data.channels.map((channel) => (
            <li key={channel.id} className={styles.row} aria-label={`Channel ${channel.name}`}>
              <span className={styles.runHeader}>
                <button
                  type="button"
                  className={systemStyles.linkButton}
                  onClick={() => {
                    setEditing(channel);
                  }}
                >
                  <span className={styles.name}>{channel.name}</span>
                </button>
                <span className={systemStyles.badge}>{labelOf(channelTypes, channel.type)}</span>
                {!channel.enabled && <span className={systemStyles.badge}>Off</span>}
              </span>
              <span className={styles.description}>{channel.summary}</span>
              {channel.lastError !== '' ? (
                <span className={alertStyles.problem}>Last send failed: {channel.lastError}</span>
              ) : (
                channel.lastSentAt !== null && (
                  <span className={styles.muted}>
                    Last sent {formatDateTime(channel.lastSentAt)}
                  </span>
                )
              )}
              <span className={alertStyles.actions}>
                <Button
                  variant="secondary"
                  isBusy={test.isPending && test.variables === channel.id}
                  onClick={() => {
                    test.mutate(channel.id, {
                      onSuccess: () => toast.success(`Sent a test to ${channel.name}`),
                      onError: (error) => toast.error(messageFor(error)),
                    });
                  }}
                >
                  Send test
                </Button>
                <button
                  type="button"
                  className={systemStyles.linkButton}
                  onClick={() => {
                    setDeleting(channel);
                  }}
                >
                  Delete
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
      {editing !== undefined && (
        <ChannelDialog
          channel={editing === 'new' ? undefined : editing}
          onClose={() => {
            setEditing(undefined);
          }}
          onSave={(request, done) => {
            save.mutate(request, {
              onSuccess: () => {
                toast.success('Channel saved');
                setEditing(undefined);
              },
              onError: done,
            });
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== undefined}
        onOpenChange={(isOpen) => {
          if (!isOpen) {
            setDeleting(undefined);
          }
        }}
        title={`Delete ${deleting?.name ?? 'this channel'}?`}
        description="Rules that used it stop sending there."
        confirmLabel="Delete"
        onConfirm={() => {
          if (deleting !== undefined) {
            remove.mutate(deleting.id, {
              onSuccess: () => toast.success('Channel deleted'),
              onError: (error) => toast.error(messageFor(error)),
            });
          }
        }}
      />
    </section>
  );
}
