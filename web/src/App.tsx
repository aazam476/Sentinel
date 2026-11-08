// SPDX-License-Identifier: Apache-2.0
import { CSPProvider } from '@base-ui/react/csp-provider';
import { QueryClientProvider } from '@tanstack/react-query';
import { useState } from 'react';
import { createBrowserRouter, RouterProvider } from 'react-router';
import { Toaster } from 'sonner';

import { newQueryClient } from './api/queryClient';
import { routerBasename } from './routing/destination';
import { routes } from './routing/routes';

/**
 * App is the root of the web UI. Base UI is told not to add inline <style>
 * elements, which the server's Content-Security-Policy refuses.
 */
export function App() {
  const [queryClient] = useState(newQueryClient);
  const [router] = useState(() =>
    createBrowserRouter(routes, { basename: routerBasename(document.baseURI) }),
  );

  return (
    <CSPProvider disableStyleElements>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
        <Toaster theme="system" position="bottom-center" richColors />
      </QueryClientProvider>
    </CSPProvider>
  );
}
