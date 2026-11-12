// SPDX-License-Identifier: Apache-2.0
import { useOutletContext } from 'react-router';

import type { SessionResponse } from '../../api/types.gen';
import styles from './AccountPage.module.css';
import { AuthenticatorSection } from './AuthenticatorSection';
import { PasskeysSection } from './PasskeysSection';
import { SessionsSection } from './SessionsSection';

/** AccountPage is where users manage their second factors and sessions. */
export function AccountPage() {
  const session = useOutletContext<SessionResponse>();

  return (
    <div className={styles.page}>
      <h1 className={styles.title}>Account</h1>
      <AuthenticatorSection session={session} />
      <PasskeysSection session={session} />
      <SessionsSection />
    </div>
  );
}
