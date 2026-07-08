export type BodySnapshot = {
  text?: string;
  base64?: string;
  bytes: number;
  captured_bytes: number;
  truncated: boolean;
};

export type ExchangeRecord = {
  id: number;
  request_id?: string;
  started_at: string;
  completed_at: string;
  duration_millis: number;
  method: string;
  host?: string;
  url: string;
  target_url?: string;
  status_code: number;
  request_headers?: Record<string, string[]>;
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
