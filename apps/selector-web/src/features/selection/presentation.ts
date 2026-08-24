export function formatMoney(amount: number, currency: string) {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount / 100);
}

export function remaining(until?: string, now = Date.now()) {
  if (!until) return '--:--';
  const seconds = Math.max(0, Math.floor((new Date(until).getTime() - now) / 1000));
  return `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`;
}

export function serverOffset(serverTime?: string, clientNow = Date.now()) {
  if (!serverTime) return 0;
  const parsed = Date.parse(serverTime);
  return Number.isFinite(parsed) ? parsed - clientNow : 0;
}

const uuidTokenPattern = /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi;
const opaqueHexTokenPattern = /\b[0-9a-f]{24,}\b/gi;
const publicIDTokenPattern =
  /\b(?:evt|tkt|adm|usr|ven|sec|row|seat|cred|inv|ptr|lay|res|sal)_[a-z0-9_-]+\b/gi;

export function humanLabel(value: string | null | undefined, fallback: string) {
  const readable = (value ?? '')
    .trim()
    .replace(uuidTokenPattern, ' ')
    .replace(opaqueHexTokenPattern, ' ')
    .replace(publicIDTokenPattern, ' ')
    .replace(/\s+/g, ' ')
    .replace(/[\s·|:/–—-]+$/g, '')
    .trim();
  return readable || fallback;
}
