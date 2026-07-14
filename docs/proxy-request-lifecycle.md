# Proxy request lifecycle

## Identity

- `trace_id`：32 字符小写十六进制字符串，标识一个逻辑代理请求，所有 retry attempt 共用。
- `attempt_id`：`<trace_id>:a<N>`，标识一次完整尝试（指令解析，以及解析成功后的可选上游 `RoundTrip`）。
- `record_id`：`<trace_id>:<sequence>`，ACK 丢失导致重发时保持不变，外部存储必须据此幂等。
- `sequence`：单个 trace 内从 1 开始严格递增。不同 trace 不保证全局顺序。

`trace_id` 同时用于活动请求 Control API 和 `X-Dproxy-Trace-ID` 响应头。

活动控制表由 `proxy.retry.max-active-requests` 限制；达到上限的新请求会在发起上游连接前返回 `503 active_request_capacity_unavailable`。

## Fluent tags

| Tag suffix | Kinds |
| --- | --- |
| `lifecycle` | `request.started`, `directive.resolve.started`, `directive.resolve.finished`, `directive.resolve.failed`, `retry.requested`, `request.completed` |
| `request.headers` | `request.headers` |
| `request.metadata` | `request.metadata.bound`, `request.metadata.changed` |
| `request.body` | `request.body.chunk`, `request.body.end` |
| `attempt` | `attempt.started`, `attempt.upstream.started`, `attempt.finished` |
| `response.headers` | `response.headers` |
| `response.body` | `response.body.chunk`, `response.body.end` |
| `response.sse` | `response.sse.event`, `response.sse.comment` |

每条记录包含 `schema_version=dproxy.capture.v1`、`record_id`、`trace_id`、可选 `attempt_id`、`instance_id`、`sequence`、`kind`、RFC3339Nano `occurred_at` 和 `data`。

Remote token 的 envelope 与 `RemoteSpec` 只在请求进入时校验一次；每个 attempt 都重新读取、解码、校验并编译远端 payload。每次解析记录 sanitized endpoint、backend/key、耗时、payload SHA-256、最终 target，以及 target/plan 是否相对上一次发生变化；不输出原始远端 payload。Inline token 只编译一次并在 attempt 间深拷贝 plan。

精确名称的 `X-Dproxy-*` directive header op 会被解析为请求 Metadata，并在第一次成功解析时输出 `request.metadata.bound`。后续 Remote directive 返回不同 Metadata 时输出 `request.metadata.changed`，逻辑请求继续使用首次绑定值。Metadata 和匿名 retry selector 都按照 header 脱敏规则输出到 Capture，且不会发送给上游。

活动状态依次为 `resolving_directive`、首次请求特有的 `buffering_body`、`awaiting_response`，外部介入后短暂进入 `retry_requested`。只有 `awaiting_response` 可重试，阈值从实际发起上游 `RoundTrip` 时开始计算。匿名请求方使用 `POST /api/public/request-retries`；认证后的管理端使用 `GET /api/control/proxy-requests`、`GET /api/control/proxy-requests/{trace_id}` 和 `POST /api/control/proxy-requests/{trace_id}/retries`。

## Body records

正文 chunk 的 `data` 使用 Base64，另含 `offset`、`length` 和 `encoding=base64`。end 记录包含 `total_bytes`、chunk 数及 SHA-256。Request body 在写入 retry 临时文件时输出；response body 只记录实际成功写给下游的字节。

Header 名称和 URL query 参数按照 `proxy.capture.redact-headers` 与 `redact-query` 的大小写不敏感 glob 脱敏。Body 默认不脱敏。

## SSE

上游请求强制 `Accept-Encoding: identity`。Capture 支持 BOM、LF、CRLF、CR、多行 `data`、`event`、`id`、`retry` 和 comment heartbeat。每个解析事件包含独立 `sse_event_id`、`sse_sequence` 和上游 `id`。

响应字节先写入并 flush 给下游，然后同步输出 raw body 与语义事件。超过 `max-sse-event-bytes` 的事件标记为 `truncated=true`；完整原始内容仍可从对应 response body chunk 重组。

## Delivery

`fluent-logger-golang` 使用 `Async=false`、MessagePack、亚秒时间戳和可选 Forward ACK。多个同步连接按 `trace_id` 分片，以保持单请求顺序并允许请求间并发。

Capture 不提供事务性持久化。推荐本地 Fluentd Unix socket 配合文件 buffer。运行期输出失败不会中断代理流量；Forward ACK 丢失可能产生重复记录，接收端必须按 `record_id` 去重。
