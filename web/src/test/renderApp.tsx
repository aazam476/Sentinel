// SPDX-License-Identifier: Apache-2.0
import { CSPProvider } from '@base-ui/react/csp-provider';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { VirtuosoMockContext } from 'react-virtuoso';
import { Toaster } from 'sonner';

import { routes } from '../routing/routes';

/**
 * renderApp renders the whole app, with its real routes, at path. Lists
 * that virtualize render as if the window were 800px tall, because jsdom
 * has no layout.
 */
export function renderApp(path: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const view = render(
    <CSPProvider disableStyleElements>
      <VirtuosoMockContext.Provider value={{ viewportHeight: 800, itemHeight: 80 }}>
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
          <Toaster />
        </QueryClientProvider>
      </VirtuosoMockContext.Provider>
    </CSPProvider>,
  );
  return { ...view, router, queryClient };
}
