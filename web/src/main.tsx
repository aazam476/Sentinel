// SPDX-License-Identifier: Apache-2.0
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './App';
import { recoverFromPreloadErrors } from './routing/preloadRecovery';
import './styles/tokens.css';
import './styles/global.css';
import 'sonner/dist/styles.css';
import './styles/toasts.css';

const rootElement = document.getElementById('root');
if (rootElement === null) {
  throw new Error('index.html is missing the #root element');
}

recoverFromPreloadErrors(window, {
  storage: (() => {
    try {
      return window.sessionStorage;
    } catch {
      return undefined;
    }
  })(),
  reload: () => {
    window.location.reload();
  },
});

createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
