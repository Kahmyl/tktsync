import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { AppErrorBoundary } from '@tktsync/ui';
import { SelectionPage } from './features/selection/SelectionPage';
import { queryClient } from './features/selection/queryClient';
import './styles.css';

const root = document.getElementById('root');

if (root) {
  createRoot(root).render(
    <StrictMode>
      <AppErrorBoundary>
        <QueryClientProvider client={queryClient}>
          <SelectionPage />
        </QueryClientProvider>
      </AppErrorBoundary>
    </StrictMode>,
  );
}
