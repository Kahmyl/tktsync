import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { AppShell } from '@tktsync/ui';
import './styles.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppShell title="TktSync Scanner" />
  </StrictMode>,
);
