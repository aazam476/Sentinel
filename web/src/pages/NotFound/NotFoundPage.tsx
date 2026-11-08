// SPDX-License-Identifier: Apache-2.0
import { Link } from 'react-router';

/** NotFoundPage answers URLs the app doesn't know. */
export function NotFoundPage() {
  return (
    <div>
      <h1>Page not found</h1>
      <p>
        There’s nothing at this address. <Link to="/">Go to the overview</Link>.
      </p>
    </div>
  );
}
