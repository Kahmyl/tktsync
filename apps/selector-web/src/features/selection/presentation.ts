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
