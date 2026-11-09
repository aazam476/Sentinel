// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { NavLink, Outlet, useOutletContext } from 'react-router';

import type { SessionResponse } from '../../api/types.gen';
import machineStyles from '../Machine/MachinePage.module.css';
import styles from '../Scripts/Scripts.module.css';

const sections = [
  { to: '/alerts', label: 'Alerts', end: true },
  { to: '/alerts/rules', label: 'Rules', end: true },
  { to: '/alerts/channels', label: 'Channels', end: true },
];

/**
 * AlertsPage is every machine's alerts and, for admins, the rules that
 * raise them and the channels that send them (§9.9).
 */
export function AlertsPage() {
  const session = useOutletContext<SessionResponse>();
  const isAdmin = session.user.role === 'admin';
  return (
    <div className={styles.page}>
      <h1 className={styles.title}>Alerts</h1>
      {isAdmin && (
        <nav className={machineStyles.subTabs} aria-label="Alert sections">
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
      )}
      <Outlet context={session} />
    </div>
  );
}
