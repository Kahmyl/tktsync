import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { AppErrorBoundary } from '@tktsync/ui';
import { installBrowserTelemetry } from '@tktsync/api-client';
import { App } from './app/App';
import { queryClient } from './app/queryClient';
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
          <App />
        </QueryClientProvider>
      </AppErrorBoundary>
    </StrictMode>,
  );
}
