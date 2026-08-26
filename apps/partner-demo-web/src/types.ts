export type Price = { amount_minor: number; currency: string };
export type Event = {
  id: string;
  name: string;
  state: string;
  starts_at?: string | null;
  venue_name?: string | null;
  address_text?: string | null;
  starting_price?: Price | null;
};
export type ReservationItem = {
  id: string;
  inventory_kind: 'RESERVED' | 'GA';
  inventory_id: string;
  quantity: number;
  unit_amount_minor: number;
  currency: string;
  price_tier_label?: string | null;
  display: { section?: string; row?: string; seat?: string; table?: string; label?: string };
};
export type Order = {
  event: Event;
  reservation: {
    id: string;
    status: string;
    hold_expires_at: string;
    server_time: string;
    items: ReservationItem[];
    total: Price;
  };
};
export type TicketResult = Order & {
  confirmation: {
    sale: { id: string; confirmed_at: string; partner_order_ref: string };
    tickets: { id: string; status: string }[];
  };
  credentials: { ticket_id: string; status: string; qr_url: string }[];
  scanner_url: string;
};
