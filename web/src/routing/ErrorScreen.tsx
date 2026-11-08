// SPDX-License-Identifier: Apache-2.0
import { AuthLayout } from '../components/AuthLayout/AuthLayout';
import { Button } from '../components/Button/Button';
import { messageFor } from '../components/FormError/messageFor';

interface ErrorScreenProps {
  error: unknown;
}

/** ErrorScreen replaces the page when its first data can't be loaded. */
export function ErrorScreen({ error }: ErrorScreenProps) {
  return (
    <AuthLayout title="Sentinel can’t be reached" description={<p>{messageFor(error)}</p>}>
      <Button
        isFullWidth
        onClick={() => {
          window.location.reload();
        }}
      >
        Try again
      </Button>
    </AuthLayout>
  );
}
