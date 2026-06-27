# llm-relay-dproxy

`llm-relay-dproxy` is a directive-driven data-plane proxy for LLM relay traffic.

The service is intentionally split into two HTTP surfaces:

- Control plane: Huma API under `/api/*`, currently `/api/health`, `/api/openapi.json`, and `/api/docs`.
- Data plane: raw `net/http` reverse proxy under `/proxy/*`.

Proxy traffic does not go through Huma so streaming responses, request bodies, and upstream headers stay under direct `net/http` control.

## Directive Token

The only directive source is:

```http
Authorization: Bearer dpx1.<base64url-json>
```

Bearer tokens without the `dpx1.` prefix are treated as non-directive tokens and are not decoded.

Payload schema:

```json
{
  "version": 1,
  "kind": "directive-proxy.directive",
  "target": {
    "url": "https://api.example.com/v1",
    "join_path": true
  },
  "transport": {
    "proxy": "socks5://user:pass@127.0.0.1:1080"
  },
  "headers": {
    "mode": "patch",
    "ops": [
      { "op": "=", "name": "Authorization", "values": ["Bearer upstream-token"] },
      { "op": "=", "name": "X-Tenant", "values": ["tenant-a"] }
    ]
  },
  "labels": {
    "trace_id": "trace-123"
  }
}
```

Use `proxydirective.Encode` to produce the complete `dpx1.` token.

When a directive is accepted, inbound `Authorization`, `X-Client-Request-Id`, and `M-Runtime-*` headers are removed before forwarding. If upstream needs its own `Authorization`, add it through directive header ops.

## Run

```bash
go run . server
```

Default listen address is `:40174`.

Useful endpoints:

```text
GET /api/health
GET /api/openapi.json
GET /api/docs
ANY /proxy/*
```

## Verify

```bash
go test ./...
go test -count=1 ./internal/testutil/tddcheck
```
