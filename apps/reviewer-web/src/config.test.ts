import { describe, expect, it } from 'vitest';
import { getReviewConfig } from './config';

describe('review configuration', () => {
  it('uses every supplied deployed URL without introducing localhost', () => {
    const config = getReviewConfig({
      VITE_ADMIN_PUBLIC_URL: 'https://admin.example.com',
      VITE_PARTNER_DEMO_PUBLIC_URL: 'https://tickets.example.com',
      VITE_SELECTOR_PUBLIC_URL: 'https://selector.example.com',
      VITE_SCANNER_PUBLIC_URL: 'https://scanner.example.com',
      VITE_DOCS_PUBLIC_URL: 'https://docs.example.com',
      VITE_SOURCE_URL: 'https://github.com/Kahmyl/tktsync',
    } as unknown as ImportMetaEnv);
    expect(Object.values(config).join(' ')).not.toContain('localhost');
    expect(config.partner).toBe('https://tickets.example.com');
  });
});
