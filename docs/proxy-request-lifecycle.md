# Proxy request lifecycle and observability

## Identity

- `trace_id`：32 字符小写十六进制字符串，标识一个逻辑代理请求，所有 retry attempt 共用。
- `record_id`：`<trace_id>:<sequence>`，输出重试时保持不变，接收端应据此幂等。
- `sequence`：单个 trace 内从 1 开始严格递增；所有观测插件共享同一序列。
- `attempt`：产生该 Record 的上游尝试序号；请求级 Record 可以省略。

每条外部 Record 包含 `schema_version=dproxy.event.v1`、`plugin`、`topic`、`record_id`、`trace_id`、可选 `attempt`、`instance_id`、`sequence`、RFC3339Nano `occurred_at` 和 `data`。

## Signal pipeline

Proxy、RetryTransport 和 downstream ResponseWriter 只产生进程内 Signal。Signal 中的 body slice 是 borrowed memory，只在插件回调期间有效；插件必须同步解析或复制。插件生成拥有完整数据所有权的 Record 后，Pipeline 才将其放入输出队列。

响应有两套明确边界：

- `UpstreamBodyChunk`：代理从上游读取到的字节，供 LLM Usage 等协议观测插件使用。
- `DownstreamBodyChunk`：成功写给客户端的字节，供 Capture 审计使用。

所有上游请求强制 `Accept-Encoding: identity`。非 identity 编码的 LLM 响应不会被解析。

## Built-in capture plugin

`builtin.capture` 产生 `capture.**` topics，包括：

- `capture.request.started`、`capture.request.headers`、`capture.request.metadata.*`；
- `capture.request.body.chunk/end`；
- `capture.directive.resolve.*`、`capture.attempt.*`、`capture.retry.requested`；
- `capture.response.headers`、`capture.response.body.chunk/end`；
- `capture.response.sse.event/comment`；
- `capture.request.completed`。

正文 chunk 使用 Base64，包含绝对 offset、length 和 chunk index；end Record 包含总字节数、chunk 数和 SHA-256。Response body 只记录实际成功写给下游的字节。

Header 和 URL query 按插件配置的大小写不敏感 glob 脱敏。Body 默认不脱敏。SSE parser 支持 BOM、LF、CRLF、CR、多行 data、event、id、retry 和 comment；超过单事件上限时语义事件标记为 truncated，原始 downstream body 仍可重组。

## Built-in LLM usage plugin

`builtin.llmusage` 只在 directive 的当前 attempt 包含以下配置时启用：

```json
{
  "plugins": {
    "llmusage": {
      "protocol": "openai.responses",
      "labels": {
        "provider": "openai"
      }
    }
  }
}
```

支持 `auto`、`openai.responses`、`openai.chat-completions`、`anthropic.messages` 和 `google.generate-content`。Format 根据 `Content-Type` 选择 JSON 或 SSE。插件产生：

- `llm.usage.observed`：规范化 token counters、response ID、model、total source 和精确保留的 `raw_usage_json`；
- `llm.usage.not_observed`：响应合法结束但没有报告 usage，例如 Chat stream 未启用 `stream_options.include_usage`；
- `llm.usage.failed`：显式启用的响应无法解析或资源限制被触发。

协议表示 wire contract，不表示实际计费供应商。labels 由 directive 明确提供，插件不猜测 provider 或价格。

## Outputs and delivery

Output 按 topic route 接收 Record。每个 output 按 `trace_id` 分片到固定 worker，以保持单 trace 顺序；队列同时限制记录数和总字节数。队列满时丢弃新 Record 并将 output health 标记为 degraded，代理请求继续执行。

Fluent output 将 Record topic 作为 tag suffix，支持 MessagePack、亚秒时间戳和 `unconfirmed`/`at-least-once`。推荐本机 Fluentd Unix socket 配合文件 buffer。Forward ACK 丢失可能产生重复记录，接收端必须按 `record_id` 去重。

启动阶段启用的 required output 无法连接会导致服务启动失败。运行阶段输出失败、队列溢出、插件 panic 会反映在 `/health.observability`；单个 LLM payload 的解析失败属于数据事件，不会把插件全局健康状态标成 degraded。
