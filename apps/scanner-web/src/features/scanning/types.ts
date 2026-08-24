export type ScannerEvent = {
  id: string;
  name: string;
  state: string;
  starts_at?: string | null;
  ends_at?: string | null;
  timezone_name?: string | null;
  venue_name: string;
  address_text?: string | null;
};

export type ScanResult = {
  result: string;
  ticket?: {
    display: { section?: string; row?: string; seat?: string };
  };
  admitted_at?: string;
  previous_admission?: { admitted_at: string };
};

export type RecentScan = {
  id: string;
  title: string;
  detail: string;
  tone: 'success' | 'warning' | 'danger';
  time: string;
};
