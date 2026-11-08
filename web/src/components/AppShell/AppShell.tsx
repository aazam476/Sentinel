// SPDX-License-Identifier: Apache-2.0
import clsx from 'clsx';
import { NavLink, Outlet } from 'react-router';

import { useLogout } from '../../api/session';
import type { SessionResponse } from '../../api/types.gen';
import styles from './AppShell.module.css';

interface AppShellProps {
  session: SessionResponse;
}

/** AppShell is the frame around every signed-in page. */
export function AppShell({ session }: AppShellProps) {
  const logout = useLogout();
  const linkClass = ({ isActive }: { isActive: boolean }) =>
    clsx(styles.link, isActive && styles.active);

  return (
    <div className={styles.shell}>
      <header className={styles.header}>
        <span className={styles.wordmark}>Sentinel</span>
        <nav className={styles.nav} aria-label="Main">
          <NavLink to="/" end className={linkClass}>
            Overview
          </NavLink>
          <NavLink to="/account" className={linkClass}>
            Account
          </NavLink>
          {session.user.role === 'admin' && (
            <NavLink to="/audit" className={linkClass}>
              Audit log
            </NavLink>
          )}
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
