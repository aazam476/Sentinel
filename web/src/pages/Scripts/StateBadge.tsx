// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';

import systemStyles from '../Machine/SystemTab.module.css';
import { stateNames, stateTone } from './scriptText';

/** RunState shows a run's state with a colored dot. */
export function RunState({ state }: { state: string }) {
  const tone = stateTone(state);
  return (
    <span
      className={clsx(
        systemStyles.badge,
        tone === 'good' && systemStyles.running,
        tone === 'bad' && systemStyles.failed,
        tone === 'warning' && systemStyles.warning,
      )}
    >
      {stateNames[state] ?? state}
    </span>
  );
}
