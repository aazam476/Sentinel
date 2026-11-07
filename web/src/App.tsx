// SPDX-License-Identifier: Apache-2.0
import { HelloPage } from './pages/Hello/HelloPage';

/**
 * App is the root of the web UI. Routing arrives once there is more than one
 * screen; until then it renders the placeholder page directly.
 */
export function App() {
  return <HelloPage />;
}
