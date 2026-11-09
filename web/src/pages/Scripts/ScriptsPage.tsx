// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { NavLink, Outlet, useOutletContext } from 'react-router';

import type { SessionResponse } from '../../api/types.gen';
import machineStyles from '../Machine/MachinePage.module.css';
import styles from './Scripts.module.css';

const sections = [
  { to: '/scripts', label: 'Library', end: true },
  { to: '/scripts/runs', label: 'Runs', end: false },
  { to: '/scripts/schedules', label: 'Schedules', end: true },
];

/** ScriptsPage is the script library, its runs and its schedules (§9.8). */
export function ScriptsPage() {
  const session = useOutletContext<SessionResponse>();
  if (session.user.role === 'viewer') {
    return (
      <div className={styles.page}>
        <h1 className={styles.title}>Scripts</h1>
        <p className={styles.muted}>Operators and admins run scripts; viewers can’t see them.</p>
      </div>
    );
  }
  return (
    <div className={styles.page}>
      <h1 className={styles.title}>Scripts</h1>
      <nav className={machineStyles.subTabs} aria-label="Script sections">
        {sections.map((section) => (
          <NavLink
            key={section.to}
            to={section.to}
            end={section.end}
            className={({ isActive }) =>
              clsx(machineStyles.subTab, isActive && machineStyles.subTabActive)
            }
          >
            {section.label}
          </NavLink>
        ))}
      </nav>
      <Outlet context={session} />
    </div>
  );
}
