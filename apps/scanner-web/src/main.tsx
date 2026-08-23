import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ScannerPage } from './features/scanning/ScannerPage';
import './styles.css';

const root = document.getElementById('root');

if (root) {
  createRoot(root).render(
    <StrictMode>
      <ScannerPage />
    </StrictMode>,
  );
}
