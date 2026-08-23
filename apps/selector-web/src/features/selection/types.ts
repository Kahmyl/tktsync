export type Offer = {
  offer_id: string;
  available_quantity?: number;
  price: {
    amount_minor: number;
    currency: string;
  };
};

export type SelectableOffer = Offer & {
  kind: 'reserved' | 'ga';
  label: string;
  inventory_id: string;
  section_id?: string;
  row?: string;
  seat?: string;
};

export type Layout = {
  event_id: string;
  geometry?: unknown;
  reserved_units: Array<{
    inventory_id: string;
    section_id?: string;
    row: string;
    seat: string;
    display_label?: string;
  }>;
  ga_pools: Array<{
    inventory_id: string;
    section_id?: string;
    name: string;
    capacity: number;
  }>;
};

export type Availability = {
  reserved_units: Array<{
    inventory_id: string;
    section_id?: string;
    row: string;
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
  starts_at?: string;
};

export type Hold = {
  id: string;
  status: string;
  hold_expires_at: string;
  reservation_token: string;
  items: Array<{
    id: string;
    inventory_id: string;
    quantity: number;
  }>;
  total: {
    amount_minor: number;
    currency: string;
  };
  return_url: string;
};
