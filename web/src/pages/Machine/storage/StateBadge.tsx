// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';

import systemStyles from '../SystemTab.module.css';
import { healthTone } from './storageText';

/** StateBadge shows a pool's or device's state, with a colored dot. */
export function StateBadge({ state }: { state: string }) {
  const tone = healthTone(state);
  return (
    <span
      className={clsx(
        systemStyles.badge,
        tone === 'good' && systemStyles.running,
        tone === 'warning' && systemStyles.warning,
        tone === 'bad' && systemStyles.failed,
      )}
    >
      {state}
    </span>
  );
}
