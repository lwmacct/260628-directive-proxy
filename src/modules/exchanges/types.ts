export type ActiveProxyRequest = {
  trace_id: string;
	has_retry_id: boolean;
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
  retryable: boolean;
  max_attempts: number;
};

export type ActiveProxyRequestSnapshot = {
  server_time: string;
  items: ActiveProxyRequest[];
};
