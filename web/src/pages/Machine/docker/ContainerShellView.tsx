// SPDX-License-Identifier: Apache-2.0
import { useState } from 'react';
import { Link, useParams } from 'react-router';

import { containerTerminal } from '../../../api/terminal';
import { useMachine } from '../machineContext';
import styles from '../SystemTab.module.css';
import { TerminalView } from '../TerminalTab';

/** ContainerShellView is a shell in a container, in the same terminal as the machine's. */
export function ContainerShellView() {
  const { session, machine } = useMachine();
  const { container = '' } = useParams();
  const [attempt, setAttempt] = useState(0);
  return (
    <div className={styles.tab}>
      <Link to=".." className={styles.muted}>
        ← Containers
      </Link>
      <TerminalView
        key={attempt}
        endpoint={containerTerminal(machine.id, container)}
        session={session}
        onRestart={() => {
          setAttempt((n) => n + 1);
        }}
      />
    </div>
  );
}
