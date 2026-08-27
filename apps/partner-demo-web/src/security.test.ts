import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const server = readFileSync(new URL('../server.mjs', import.meta.url), 'utf8');

describe('checkout security boundary', () => {
  it('keeps Partner credentials in server environment configuration', () => {
    expect(server).toContain('process.env.PARTNER_DEMO_CREDENTIAL');
    expect(server).not.toMatch(/json\([^\n]+partnerCredential/);
  });
  it('accepts the reservation token from POST and seals checkout state in an HttpOnly cookie', () => {
    expect(server).toContain("form.get('reservation_token')");
    expect(server).toContain("createCipheriv('aes-256-gcm'");
    expect(server).toContain('northstar_checkout=${seal(session)}');
    expect(server).toContain('HttpOnly; SameSite=Lax');
    expect(server).not.toContain('localStorage');
    expect(server).not.toMatch(/console\.log\([^\n]*(reservationToken|partnerCredential)/);
  });
  it('stores reusable Partner connections encrypted and only exposes sanitized identity', () => {
    expect(server).toContain('PARTNER_DEMO_SESSION_SECRET');
    expect(server).toContain('northstar_connections=${seal(value)}');
    expect(server).toContain('const items = saved.items.map(({ id, name })');
    expect(server).not.toContain('localStorage');
  });
  it('distinguishes invalid credentials from deployment connectivity failures', () => {
    expect(server).toContain('error?.status === 401 || error?.status === 403');
    expect(server).toContain("return 'configuration'");
    expect(server).toContain("return 'unavailable'");
    expect(server).toContain('Array.isArray(result.items)');
    expect(server).toContain('process.env.API_PUBLIC_URL || process.env.VITE_API_BASE_URL');
  });
  it('calls checkout before confirmation and retrieves hosted QR credentials', () => {
    expect(server.indexOf('}/checkout`')).toBeLessThan(server.indexOf('}/confirm`'));
    expect(server).toContain('/credential`');
  });
});
