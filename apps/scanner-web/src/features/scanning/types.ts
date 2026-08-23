export type ScanResult = {
  result: string;
  ticket?: {
    id: string;
    display: { section_name?: string; row_label?: string; seat_label?: string };
  };
  admission_id?: string;
  admitted_at?: string;
  previous_admission?: { admitted_at: string; gate_reference: string };
  scan_attempt_id?: string;
};
