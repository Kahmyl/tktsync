import type { ScanResult } from './types';

const uuidTokenPattern = /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi;
const opaqueHexTokenPattern = /\b[0-9a-f]{24,}\b/gi;
const publicIDTokenPattern =
  /\b(?:evt|event|tkt|ticket|adm|admission|scan|usr|user|ven|venue|sec|section|row|seat|dev|device|cred|credential|inv|inventory)_[a-z0-9_-]+\b/gi;

export function humanLabel(value: string | null | undefined, fallback: string) {
  const label = value?.trim() ?? '';
  const readable = label
    .replace(uuidTokenPattern, ' ')
    .replace(opaqueHexTokenPattern, ' ')
    .replace(publicIDTokenPattern, ' ')
    .replace(/\(\s*\)|\[\s*\]|\{\s*\}/g, ' ')
    .replace(/\s+/g, ' ')
    .replace(/[\s·|:/–—-]+$/g, '')
    .trim();
  return readable || fallback;
}

export type OutcomePresentation = {
  tone: 'ready' | 'success' | 'warning' | 'danger';
  title: string;
  description: string;
};

export function ticketLocation(result?: ScanResult) {
  const display = result?.ticket?.display;
  if (!display) return '';
  const section = humanLabel(display.section, '');
  const seat = humanLabel(display.seat, '');
  const row = humanLabel(display.row, '');
  return [section, row ? `Row ${row}` : '', seat ? `Seat ${seat}` : ''].filter(Boolean).join(' · ');
}

export function outcomePresentation(
  result: ScanResult | undefined,
  cannotVerify: boolean,
  eventName: string,
): OutcomePresentation {
  if (cannotVerify) {
    return {
      tone: 'danger',
      title: "Can't verify ticket",
      description:
        'Check your connection and try again. Do not admit this guest until the ticket can be verified.',
    };
  }
  switch (result?.result) {
    case 'ADMITTED':
    case 'MANUAL_OVERRIDE_ADMITTED':
      return {
        tone: 'success',
        title: 'Admit guest',
        description: ticketLocation(result) || 'Ticket checked successfully.',
      };
    case 'TICKET_ALREADY_ADMITTED':
      return {
        tone: 'warning',
        title: 'Already checked in',
        description: result.previous_admission?.admitted_at
          ? `First admitted at ${new Date(result.previous_admission.admitted_at).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}`
          : ticketLocation(result),
      };
    case 'WRONG_EVENT':
      return {
        tone: 'danger',
        title: 'Wrong event',
        description: `This ticket is not valid for ${eventName}.`,
      };
    case 'EVENT_CANCELLED':
      return {
        tone: 'danger',
        title: 'Ticket not valid',
        description: 'This event is not accepting tickets.',
      };
    case 'ADMISSION_NOT_OPEN':
      return {
        tone: 'danger',
        title: 'Ticket not valid',
        description: 'Entry is not open for this event.',
      };
    case 'CREDENTIAL_REVOKED':
    case 'CREDENTIAL_SUPERSEDED':
    case 'TICKET_VOID':
    case 'TICKET_INVALID':
      return {
        tone: 'danger',
        title: 'Ticket not valid',
        description: 'Ask the guest to contact the ticket seller for help.',
      };
    default:
      return {
        tone: 'ready',
        title: 'Point the camera at the ticket QR code',
        description: 'Hold the code steady inside the frame.',
      };
  }
}
