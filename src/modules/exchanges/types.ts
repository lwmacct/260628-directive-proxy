export type BodySnapshot = {
  text?: string;
  base64?: string;
  bytes: number;
  captured_bytes: number;
  truncated: boolean;
};

export type ExchangeRecord = {
  id: number;
  started_at: string;
  completed_at: string;
  duration_millis: number;
  method: string;
  host?: string;
  url: string;
  target_url?: string;
  directive_source?: string;
  directive_key?: string;
  directive_lookup_millis?: number;
  status_code: number;
  request_headers?: Record<string, string[]>;
  outbound_request_headers?: Record<string, string[]>;
  response_headers?: Record<string, string[]>;
  request_body: BodySnapshot;
  response_body: BodySnapshot;
};

export type ExchangeSnapshot = {
  enabled: boolean;
  capacity: number;
  max_body_bytes: number;
  total: number;
  items: ExchangeRecord[];
};
