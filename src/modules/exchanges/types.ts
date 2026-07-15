export type ActiveProxyRequest = {
  trace_id: string;
	request_id?: string;
  metadata?: Record<string, string[]>;
  state: "waiting_body_memory" | "reading_body" | "resolving_directive" | "awaiting_response" | "retry_requested";
  method: string;
  url: string;
  target_url: string;
  started_at: string;
  attempt: number;
  attempt_started_at: string;
  upstream_started_at?: string;
  waiting_millis: number;
  retryable_at?: string;
  retryable: boolean;
  max_attempts: number;
};

export type ActiveProxyRequestSnapshot = {
  server_time: string;
  items: ActiveProxyRequest[];
};
