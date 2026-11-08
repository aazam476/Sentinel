// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';

import styles from './FleetPage.module.css';

interface FleetToolbarProps {
  search: string;
  onSearchChange: (search: string) => void;
  tags: string[];
  selectedTag: string | null;
  onTagChange: (tag: string | null) => void;
}

/** FleetToolbar narrows the fleet by name and by tag. */
export function FleetToolbar({
  search,
  onSearchChange,
  tags,
  selectedTag,
  onTagChange,
}: FleetToolbarProps) {
  return (
    <div className={styles.toolbar}>
      <input
        type="search"
        className={styles.search}
        placeholder="Search machines"
        aria-label="Search machines"
        value={search}
        autoCapitalize="none"
        autoCorrect="off"
        enterKeyHint="search"
        onChange={(event) => {
          onSearchChange(event.target.value);
        }}
      />
      {tags.length > 0 && (
        <div className={styles.tagFilter} role="group" aria-label="Filter by tag">
          {tags.map((tag) => (
            <button
              key={tag}
              type="button"
              aria-pressed={selectedTag === tag}
              title={tag}
              className={clsx(styles.tagButton, selectedTag === tag && styles.tagButtonActive)}
              onClick={() => {
                onTagChange(selectedTag === tag ? null : tag);
              }}
            >
              {tag}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
