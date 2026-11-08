// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { NavLink, Outlet } from 'react-router';

import { useMachine } from '../machineContext';
import machineStyles from '../MachinePage.module.css';
import styles from '../SystemTab.module.css';

const sections = [
  { to: '', label: 'Containers' },
  { to: 'stacks', label: 'Stacks' },
  { to: 'images', label: 'Images' },
  { to: 'volumes', label: 'Volumes and networks' },
];

/** DockerTab is the machine's containers, stacks, images, volumes and networks (§9.4). */
export function DockerTab() {
  const context = useMachine();
  if (!context.machine.online) {
    return <p className={styles.muted}>The machine is offline. Docker shows when it’s back.</p>;
  }
  return (
    <div className={styles.tab}>
      <nav className={machineStyles.subTabs} aria-label="Docker sections">
        {sections.map((section) => (
          <NavLink
            key={section.label}
            to={section.to}
            end
            className={({ isActive }) =>
              clsx(machineStyles.subTab, isActive && machineStyles.subTabActive)
            }
          >
            {section.label}
          </NavLink>
        ))}
      </nav>
      <Outlet context={context} />
    </div>
  );
}
