// SPDX-License-Identifier: Apache-2.0
import styles from './FormError.module.css';
import { messageFor } from './messageFor';

interface FormErrorProps {
  error: unknown;
}

/**
 * FormError shows why a request failed, read out by screen readers as soon
 * as it appears. Errors about one field are shown at that field instead.
 */
export function FormError({ error }: FormErrorProps) {
  if (error === null || error === undefined) {
    return null;
  }
  return (
    <p role="alert" className={styles.error}>
      {messageFor(error)}
    </p>
  );
}
