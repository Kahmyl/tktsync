import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { AppErrorBoundary } from '@tktsync/ui';
import { installBrowserTelemetry } from '@tktsync/api-client';
import { ScannerPage } from './features/scanning/ScannerPage';
import { queryClient } from './features/scanning/queryClient';
import './styles.css';

installBrowserTelemetry({
  endpoint: import.meta.env.VITE_BROWSER_TELEMETRY_ENDPOINT,
  application: 'scanner-web',
});

const root = document.getElementById('root');

if (root) {
  createRoot(root).render(
    <StrictMode>
      <AppErrorBoundary>
        <QueryClientProvider client={queryClient}>
          <ScannerPage />
        </QueryClientProvider>
      </AppErrorBoundary>
    </StrictMode>,
  );
}
