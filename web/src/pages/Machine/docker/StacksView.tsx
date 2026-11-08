// SPDX-License-Identifier: Apache-2.0
import { Link } from 'react-router';

import { useStacks } from '../../../api/docker';
import { countOf } from '../../../formatting/formatting';
import { canOperate, useMachine } from '../machineContext';
import styles from '../SystemTab.module.css';
import { DockerError } from './dockerError';

/** StacksView lists compose stacks: Sentinel's own and others found on the machine. */
export function StacksView() {
  const { session, machine } = useMachine();
  const stacks = useStacks(machine.id);
  if (stacks.isPending) {
    return <p className={styles.muted}>Loading stacks…</p>;
  }
  if (stacks.data === undefined) {
    return <DockerError error={stacks.error} />;
  }
  const canControl = canOperate(session);
  return (
    <section className={styles.tab} aria-label="Stacks">
      <div className={styles.toolbar}>
        <p className={styles.count}>
          {countOf(stacks.data.stacks.length, 'stack', 'stacks')} · kept in {stacks.data.dir}
        </p>
        {canControl && (
          <Link to="new" className={styles.linkButton}>
            New stack
          </Link>
        )}
      </div>
      {stacks.data.stacks.length === 0 && (
        <p className={styles.muted}>No stacks yet. Create one by pasting a compose file.</p>
      )}
      <ul className={styles.list}>
        {stacks.data.stacks.map((stack) => {
          const running = stack.containers.running ?? 0;
          const total = Object.values(stack.containers).reduce((sum, count) => sum + count, 0);
          return (
            <li key={stack.name} className={styles.row}>
              <span className={styles.rowMain}>
                {canControl ? (
                  <Link to={encodeURIComponent(stack.name)} className={styles.name}>
                    {stack.name}
                  </Link>
                ) : (
                  <span className={styles.name}>{stack.name}</span>
                )}
                <span className={styles.description} title={stack.configFiles.join(', ')}>
                  {stack.managed ? 'Sentinel' : 'Started outside Sentinel'}
                  {stack.configFiles.length > 0 && ` · ${stack.configFiles[0] ?? ''}`}
                </span>
                <span className={styles.meta}>
                  <span>
                    {total === 0
                      ? 'Not running'
                      : `${String(running)} of ${countOf(total, 'container', 'containers')} running`}
                  </span>
                </span>
              </span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
