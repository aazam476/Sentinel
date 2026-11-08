// SPDX-License-Identifier: Apache-2.0
import { Link } from 'react-router';

import { useCreateEnrollmentToken } from '../../api/enrollment';
import { FormError } from '../../components/FormError/FormError';
import styles from './AddMachinePage.module.css';
import { InstallCommands } from './InstallCommands';
import { TokenForm } from './TokenForm';
import { TokenList } from './TokenList';

/**
 * AddMachinePage creates an enrollment token and shows the command that
 * installs the agent with it (admins only; the server checks too).
 */
export function AddMachinePage() {
  const create = useCreateEnrollmentToken();

  return (
    <div className={styles.page}>
      <Link to="/" className={styles.back} aria-label="Back to machines">
        <span aria-hidden="true">←</span> Machines
      </Link>
      <h1 className={styles.title}>Add a machine</h1>
      <p className={styles.text}>
        Create a one-time install command, then run it on the machine. The agent checks the server’s
        identity with the fingerprint in the command, so copy it exactly.
      </p>
      {create.data === undefined ? (
        <TokenForm
          isBusy={create.isPending}
          onSubmit={(request) => {
            create.mutate(request);
          }}
        />
      ) : (
        <InstallCommands created={create.data} />
      )}
      <FormError error={create.error} />
      <TokenList />
    </div>
  );
}
