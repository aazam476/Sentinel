// SPDX-License-Identifier: Apache-2.0
import { Menu } from '@base-ui/react/menu';
import clsx from 'clsx';
import { useState, type SubmitEvent } from 'react';
import { Virtuoso } from 'react-virtuoso';
import { toast } from 'sonner';

import { useCheckImages, useImages, usePullImage, useRemoveImage } from '../../../api/docker';
import type { Image } from '../../../api/protocol.gen';
import type { DockerImageUpdate } from '../../../api/types.gen';
import { Button } from '../../../components/Button/Button';
import { ConfirmDialog } from '../../../components/ConfirmDialog/ConfirmDialog';
import { messageFor } from '../../../components/FormError/messageFor';
import { countOf, formatBytes, formatRelative } from '../../../formatting/formatting';
import { useNow } from '../../../hooks/useNow';
import { canOperate, useMachine } from '../machineContext';
import styles from '../SystemTab.module.css';
import { DockerError } from './dockerError';
import { RegistryLogins } from './RegistryLogins';

/** ImagesView lists images with their update status; operators pull and remove them. */
export function ImagesView() {
  const { session, machine } = useMachine();
  const images = useImages(machine.id);
  const pull = usePullImage(machine.id);
  const check = useCheckImages(machine.id);
  const remove = useRemoveImage(machine.id);
  const [reference, setReference] = useState('');
  const [toRemove, setToRemove] = useState<Image>();
  const canControl = canOperate(session);

  if (images.isPending) {
    return <p className={styles.muted}>Loading images…</p>;
  }
  if (images.data === undefined) {
    return <DockerError error={images.error} />;
  }
  const pullImage = (name: string) => {
    const pending = toast.loading(`Pulling ${name}…`);
    pull.mutate(name, {
      onSuccess: () => toast.success(`Pulled ${name}`, { id: pending }),
      onError: (error) => toast.error(messageFor(error), { id: pending }),
    });
  };
  const handlePull = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (reference.trim() !== '') {
      pullImage(reference.trim());
      setReference('');
    }
  };
  const { updates } = images.data;
  const withUpdates = Object.values(updates).filter((u) => u.updateAvailable).length;
  return (
    <section className={styles.tab} aria-label="Images">
      {canControl && (
        <form className={styles.toolbar} onSubmit={handlePull}>
          <input
            className={styles.search}
            placeholder="nginx:1.27 or ghcr.io/owner/app:tag"
            aria-label="Image to pull"
            value={reference}
            autoCapitalize="none"
            autoCorrect="off"
            onChange={(event) => {
              setReference(event.target.value);
            }}
          />
          <Button type="submit" variant="secondary" isBusy={pull.isPending}>
            Pull
          </Button>
          <Button
            variant="secondary"
            isBusy={check.isPending}
            onClick={() => {
              check.mutate(undefined, { onError: (error) => toast.error(messageFor(error)) });
            }}
          >
            Check for updates
          </Button>
        </form>
      )}
      <p className={styles.count}>
        {countOf(images.data.images.length, 'image', 'images')}
        {withUpdates > 0 && ` · ${countOf(withUpdates, 'update', 'updates')} available`}
      </p>
      {images.data.images.length > 0 && (
        <Virtuoso
          useWindowScroll
          data={images.data.images}
          computeItemKey={(_, image) => image.id}
          className={styles.list}
          itemContent={(_, image) => (
            <ImageRow
              image={image}
              updates={updates}
              canControl={canControl}
              onPull={pullImage}
              onRemove={() => {
                setToRemove(image);
              }}
            />
          )}
        />
      )}
      <ConfirmDialog
        open={toRemove !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setToRemove(undefined);
          }
        }}
        title={`Remove ${imageName(toRemove)}?`}
        description="It can be pulled again later. Images that containers use can’t be removed."
        confirmLabel="Remove"
        onConfirm={() => {
          if (toRemove !== undefined) {
            const name = imageName(toRemove);
            remove.mutate(
              { reference: toRemove.id, force: false },
              {
                onSuccess: () => toast.success(`Removed ${name}`),
                onError: (error) => toast.error(messageFor(error)),
              },
            );
          }
        }}
      />
      {session.user.role === 'admin' && <RegistryLogins />}
    </section>
  );
}

function imageName(image: Image | undefined): string {
  if (image === undefined) {
    return '';
  }
  return image.tags[0] ?? image.id.replace('sha256:', '').slice(0, 12);
}

interface ImageRowProps {
  image: Image;
  updates: Record<string, DockerImageUpdate>;
  canControl: boolean;
  onPull: (reference: string) => void;
  onRemove: () => void;
}

function ImageRow({ image, updates, canControl, onPull, onRemove }: ImageRowProps) {
  const now = useNow();
  const name = imageName(image);
  const updatable = image.tags.filter((tag) => updates[tag]?.updateAvailable === true);
  const checked = image.tags.map((tag) => updates[tag]).find((u) => u !== undefined);
  return (
    <article className={styles.row} aria-label={name}>
      <div className={styles.rowMain}>
        <span className={styles.name}>{name}</span>
        {image.tags.length > 1 && (
          <span className={styles.description} title={image.tags.join(', ')}>
            Also {image.tags.slice(1).join(', ')}
          </span>
        )}
        <span className={styles.meta}>
          {updatable.length > 0 && (
            <span className={clsx(styles.badge, styles.warning)}>Update available</span>
          )}
          <span>{formatBytes(image.sizeBytes)}</span>
          <span>{countOf(image.containers, 'container', 'containers')}</span>
          {checked !== undefined && (
            <span title={checked.error === '' ? undefined : checked.error}>
              {checked.error === '' ? 'Checked' : 'Check failed'}{' '}
              {formatRelative(checked.checkedAt, now)}
            </span>
          )}
        </span>
      </div>
      {canControl && (
        <Menu.Root>
          <Menu.Trigger className={styles.menuTrigger} aria-label={`Actions for ${name}`}>
            ⋯
          </Menu.Trigger>
          <Menu.Portal>
            <Menu.Positioner align="end" sideOffset={4}>
              <Menu.Popup className={styles.menu}>
                {image.tags.map((tag) => (
                  <Menu.Item
                    key={tag}
                    className={styles.menuItem}
                    onClick={() => {
                      onPull(tag);
                    }}
                  >
                    Pull {tag}
                  </Menu.Item>
                ))}
                <Menu.Item
                  className={clsx(styles.menuItem, styles.menuItemDanger)}
                  onClick={onRemove}
                >
                  Remove
                </Menu.Item>
              </Menu.Popup>
            </Menu.Positioner>
          </Menu.Portal>
        </Menu.Root>
      )}
    </article>
  );
}
