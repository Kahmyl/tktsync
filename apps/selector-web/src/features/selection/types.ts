export type Offer = {
  offer_id: string;
  available_quantity?: number;
  price: { amount_minor: number; currency: string };
};

export type SelectableOffer = Offer & {
  kind: 'reserved' | 'ga';
  label: string;
  inventory_id: string;
  section_id?: string;
  section_name?: string;
  row?: string;
  table?: string;
  seat?: string;
};

export type SelectionLine = { offer: SelectableOffer; quantity: number };

export type Layout = {
  event_id: string;
  geometry?: {
    objects?: Array<{
      object_key: string;
      type: string;
      label: string;
      x: number;
      y: number;
      width: number;
      height: number;
      rotation?: number;
    }>;
  };
  reserved_units: Array<{
    inventory_id: string;
    section_id?: string;
    section_name?: string;
    section_object_key?: string;
    row?: string;
    table?: string;
    seat: string;
    display_label?: string;
  }>;
  ga_pools: Array<{
    inventory_id: string;
    section_id?: string;
    section_name?: string;
    section_object_key?: string;
    name: string;
    capacity: number;
  }>;
};

export type Availability = {
  reserved_units: Array<{
    inventory_id: string;
    section_id?: string;
    row?: string;
    seat: string;
    sellability: string;
    offer?: Offer;
  }>;
  ga_pools: Array<{
    inventory_id: string;
    section_id?: string;
    name: string;
    offers: Offer[];
  }>;
  server_time: string;
};

export type Session = {
  id: string;
  event_id: string;
  return_url: string;
  expires_at: string;
};

export type EventView = {
  name: string;
  state: string;
  starts_at?: string | null;
  ends_at?: string | null;
  venue_name?: string | null;
  address_text?: string | null;
};

export type Hold = {
  id: string;
  status: string;
  hold_expires_at: string;
  reservation_token: string;
  items: Array<{ id: string; inventory_id: string; quantity: number }>;
  total: { amount_minor: number; currency: string };
  return_url: string;
};
