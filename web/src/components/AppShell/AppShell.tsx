// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { NavLink, Outlet, useMatch } from 'react-router';

import { useFleetEvents } from '../../api/events';
import { useLogout } from '../../api/session';
import type { SessionResponse } from '../../api/types.gen';
import styles from './AppShell.module.css';

interface AppShellProps {
  session: SessionResponse;
}

/** AppShell is the frame around every signed-in page. */
export function AppShell({ session }: AppShellProps) {
  const logout = useLogout();
  useFleetEvents();
  const isOnMachinePage = useMatch('/machines/*') !== null;
  const linkClass = ({ isActive }: { isActive: boolean }) =>
    clsx(styles.link, isActive && styles.active);

  return (
    <div className={styles.shell}>
      <header className={styles.header}>
        <span className={styles.wordmark}>Sentinel</span>
        <nav className={styles.nav} aria-label="Main">
          <NavLink
            to="/"
            end
            className={({ isActive }) => linkClass({ isActive: isActive || isOnMachinePage })}
          >
            Machines
          </NavLink>
          <NavLink to="/alerts" className={linkClass}>
            Alerts
          </NavLink>
          {session.user.role !== 'viewer' && (
            <NavLink to="/scripts" className={linkClass}>
              Scripts
            </NavLink>
          )}
          <NavLink to="/account" className={linkClass}>
            Account
          </NavLink>
          {session.user.role === 'admin' && (
            <NavLink to="/updates" className={linkClass}>
              Updates
            </NavLink>
          )}
          {session.user.role === 'admin' && (
            <NavLink to="/users" className={linkClass}>
              Users
            </NavLink>
          )}
          {session.user.role === 'admin' && (
            <NavLink to="/audit" className={linkClass}>
              Audit log
            </NavLink>
          )}
          {session.user.role === 'admin' && (
            <NavLink to="/settings" className={linkClass}>
              Settings
            </NavLink>
          )}
          <NavLink to="/about" className={linkClass}>
            About
          </NavLink>
        </nav>
        <div className={styles.user}>
          <span className={styles.username} title={session.user.username}>
            {session.user.username}
          </span>
          <button
            type="button"
            className={styles.signOut}
            disabled={logout.isPending}
            onClick={() => {
              logout.mutate();
            }}
          >
            Sign out
          </button>
        </div>
      </header>
      <main className={styles.main}>
        <Outlet context={session} />
      </main>
    </div>
  );
}
