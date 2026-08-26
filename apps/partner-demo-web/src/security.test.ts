import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const server = readFileSync(new URL('../server.mjs', import.meta.url), 'utf8');

describe('checkout security boundary', () => {
  it('keeps Partner credentials in server environment configuration', () => {
    expect(server).toContain('process.env.PARTNER_DEMO_CREDENTIAL');
    expect(server).not.toMatch(/json\([^\n]+partnerCredential/);
  });
  it('accepts reservation token from POST body and stores only an opaque cookie identifier', () => {
    expect(server).toContain("form.get('reservation_token')");
    expect(server).toContain('HttpOnly; SameSite=Lax');
    expect(server).not.toContain('localStorage');
    expect(server).not.toMatch(/console\.log\([^\n]*(reservationToken|partnerCredential)/);
  });
  it('calls checkout before confirmation and retrieves hosted QR credentials', () => {
    expect(server.indexOf('}/checkout`')).toBeLessThan(server.indexOf('}/confirm`'));
    expect(server).toContain('/credential`');
  });
});
