// SPDX-License-Identifier: Apache-2.0
import { isApiError } from '../../../api/client';
import { FormError } from '../../../components/FormError/FormError';
import styles from '../SystemTab.module.css';

/**
 * DockerError explains a Docker request that failed, with a plain sentence
 * for the common case of a machine without Docker.
 */
export function DockerError({ error }: { error: unknown }) {
  if (isApiError(error, 'docker_unavailable')) {
    return (
      <p className={styles.muted}>
        Docker not detected. If it runs here under another socket (Podman, rootless Docker), set it
        in the machine’s policy file under [docker] socket.
      </p>
    );
  }
  return <FormError error={error} />;
}
