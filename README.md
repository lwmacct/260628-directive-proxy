# llm-relay-dproxy

`llm-relay-dproxy` 是面向 LLM relay 流量的指令代理 data plane。

项目当前只保留代理职责：解析 `Authorization: Bearer dpx1...` 中的 directive，按 directive 改写请求并转发到目标上游。usage 解析、审计、Kafka/HTTP 投递等观测能力已经迁移到前置的 `llm-relay-parse` 服务。

服务分为两个独立 HTTP listener：

- Control plane：默认监听 `:23198`，基于 Huma 的 `/api/*`，当前包含 `/api/health`、`/api/openapi.json`、`/api/docs`。
- Data plane：默认监听 `:23197`，基于原生 `net/http` 的 `/*` 反向代理。

代理流量不经过 Huma，避免流式响应、请求体和上游 header 被 API 框架额外处理。

## Directive Token

唯一入口是：

```http
Authorization: Bearer dpx1.<base64url-json>
```

没有 `dpx1.` 前缀的 Bearer token 会被视为非 directive token，不会尝试解码。

payload schema：

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
      {
        "op": "=",
        "name": "Authorization",
        "values": ["Bearer upstream-token"]
      },
      { "op": "=", "name": "X-Tenant", "values": ["tenant-a"] }
    ]
  }
}
```

使用 `directive.Encode` 可以生成完整的 `dpx1.` token。

directive 被接受后，入站 `Authorization`、`X-Client-Request-Id` 和 `M-Runtime-*` header 会在转发前移除。如果上游需要自己的 `Authorization`，需要通过 directive 的 header ops 显式写入。

## 运行

```bash
go run . server
```

默认 control plane 监听地址是 `:23198`，默认 data plane 监听地址是 `:23197`。

常用端点：

```text
Control plane (:23198)
  GET /api/health
  GET /api/openapi.json
  GET /api/docs

Data plane (:23197)
  ANY /*
```

## 验证

```bash
go test ./...
go test -count=1 ./internal/testutil/tddcheck
```
