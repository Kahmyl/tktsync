import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import { AppErrorBoundary } from '@tktsync/ui';
import { installBrowserTelemetry } from '@tktsync/api-client';
import { App } from './app/App';
import { queryClient } from './app/queryClient';
import { OperatorSessionProvider } from './auth/OperatorSessionContext';
import './styles.css';

installBrowserTelemetry({
  endpoint: import.meta.env.VITE_BROWSER_TELEMETRY_ENDPOINT,
  application: 'admin-web',
});

const root = document.getElementById('root');

if (root) {
  createRoot(root).render(
    <StrictMode>
      <AppErrorBoundary>
        <QueryClientProvider client={queryClient}>
          <BrowserRouter>
            <OperatorSessionProvider>
              <App />
            </OperatorSessionProvider>
          </BrowserRouter>
        </QueryClientProvider>
      </AppErrorBoundary>
    </StrictMode>,
  );
}
