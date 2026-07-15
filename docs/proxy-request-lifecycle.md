# Proxy request lifecycle and observability

## Identity

- `trace_id`：服务端生成的 canonical UUIDv7，标识一个逻辑代理请求，所有 retry attempt 共用。
- `retry_id`：调用方生成的 canonical UUIDv7 bearer retry credential；服务端只保存其 SHA-256 verifier。
- `record_id`：`<trace_id>:<sequence>`，输出重试时保持不变，接收端应据此幂等。
- `sequence`：单个 trace 内从 1 开始严格递增；所有观测插件共享同一序列。
- `attempt`：产生该 Record 的上游尝试序号；请求级 Record 可以省略。

每条外部 Record 包含 `schema_version=dproxy.event.v1`、`plugin`、`topic`、`record_id`、`trace_id`、可选 `attempt`、`instance_id`、`sequence`、RFC3339Nano `occurred_at` 和 `data`。

## Signal pipeline

Proxy、RetryTransport 和 downstream ResponseWriter 只产生进程内 Signal。流式响应 Signal 中的 body slice 是 borrowed memory，只在插件回调期间有效；插件必须同步解析或复制。请求正文例外：`RequestBodyAvailable` 暴露连续、不可变的 canonical body，异步 Record 必须取得 lease，最后一个 lease 释放时正文内存才归还。Pipeline 对每条 Record 建立共享资源引用，多个 output 共用同一 payload，最后一个 output 完成或丢弃后执行 release。

请求正文准入流程为：验证 `Content-Length` -> 进入严格 FIFO 字节预算队列 -> 获得 reservation -> 设置 body read deadline -> 一次性读取准确长度 -> 发布不可变 body。排队阶段不会调用 `Body.Read`。未知长度返回 411，单体超限返回 413，队列满或等待超时返回 503。正文只驻留内存，不写临时文件，也不做分段存储。

响应有两套明确边界：

- `UpstreamBodyChunk`：代理从上游读取到的字节，供 LLM Usage 等协议观测插件使用。
- `DownstreamBodyChunk`：成功写给客户端的字节，供 Capture 审计使用。

所有上游请求强制 `Accept-Encoding: identity`。非 identity 编码的 LLM 响应不会被解析。

## Retry protocol

Proxy Retry 是每个代理请求的固有能力。初始请求无需携带 retry identity；只有需要使用 Public retry API 时才携带 canonical UUIDv7 `Dproxy-Retry-ID`。它在 observability、resolver 和 upstream 看到请求前即被移除。公开重试端点为：

```http
PUT /api/public/retry
Dproxy-Retry-ID: <uuidv7>
If-Match: "attempt:<current_attempt>"
```

Control API 使用 `PUT /api/control/proxy-requests/{trace_id}/retry` 和相同的 `If-Match`。服务端自动计算下一 attempt；每个逻辑请求由单一 coordinator event loop 串行处理响应到达和重试命令；重复 PUT 返回首次接受的结果。POST/PATCH 缺少初始 `Idempotency-Key` 时返回 422。

Header 和 URL query 按插件配置的大小写不敏感 glob 脱敏。Body 默认不脱敏。SSE parser 支持 BOM、LF、CRLF、CR、多行 data、event、id、retry 和 comment；超过单事件上限时语义事件标记为 truncated，原始 downstream body 仍可重组。

各内置插件的 directive spec、部署配置和 topic 见 [`docs/plugin-capture.md`](plugin-capture.md)、[`docs/plugin-llmusage.md`](plugin-llmusage.md) 和 [`docs/plugin-llmperf.md`](plugin-llmperf.md)。

## Outputs and delivery

Output 按 topic route 接收 Record。每个 output 按 `trace_id` 分片到固定 worker，以保持单 trace 顺序；队列同时限制记录数和总字节数。队列满时丢弃新 Record 并将 output health 标记为 degraded，代理请求继续执行。

Fluent output 将 Record topic 作为 tag suffix，支持 MessagePack、亚秒时间戳和 `unconfirmed`/`at-least-once`。推荐本机 Fluentd Unix socket 配合文件 buffer。Forward ACK 丢失可能产生重复记录，接收端必须按 `record_id` 去重。

启动阶段启用的 required output 无法连接会导致服务启动失败。运行阶段输出失败、队列溢出、插件 panic 会反映在 `/health.observability`；单个 LLM payload 的解析失败属于数据事件，不会把插件全局健康状态标成 degraded。
