# Capture Module

`builtin.capture` 是 exchange-lifetime Module，跨越全部 Recovery RoundTrip，记录请求、响应和生命周期审计事件。

```json
{
  "program": [
    {
      "module": "builtin.capture",
      "config": {
        "body-chunk-bytes": 32768,
        "redact-headers": ["authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key"],
        "redact-query": ["access_token", "api_key", "apikey", "key", "token"]
      }
    }
  ]
}
```

配置：

- `body-chunk-bytes`：单条正文 Record 的最大字节数；`0` 使用 32 KiB，上限 1 MiB；
- `redact-headers`：需要脱敏的 header 精确名称或大小写不敏感 glob；
- `redact-query`：需要脱敏的 URL query 参数名称或 glob。

两个 redaction 列表默认均为空；Capture 会完整保留 endpoint、URL query、认证 header 和 URL userinfo。只有 directive 显式声明的 pattern 才会替换对应字段，不存在服务端静态脱敏 fallback。

Capture 订阅 request/round-trip facts、流式 `RequestBodyChunk`、downstream raw body、共享 SSE data/comment 投影和 request finish。请求正文端口使用 `ordered_lane + before_commit`，Capture 按 `body-chunk-bytes` 重新分片并以 borrowed Record 提交，Dispatcher 在入队时复制自己拥有的数据；其余端口异步执行并在 exchange lifetime 结束前 drain。

主要 topics 为 `capture.request.*`、`capture.directive.*`、`capture.round_trip.*`、`capture.recovery.*` 和 `capture.response.*`。`capture.directive.prepared` 每个请求只输出一次，data 包含 source、固定 target 和 payload digest；`capture.round_trip.started` 每次 RoundTrip 输出一次并重复这些固定事实，便于独立审计。完整 metadata 由 `dp.event.v4` Record 顶层统一提供，不在 Capture topic data 中重复；不存在 per-RoundTrip directive resolve 或 metadata changed topic。

Recovery transaction 依次输出 `capture.recovery.started`、`capture.recovery.decided` 和 `capture.recovery.finished`；三者共享 Controller callback 的 `event_id`，最终 topic 包含实际 outcome、action、delay 和错误信息。正文 chunk 使用 MessagePack binary，包含 offset、length 和 chunk index。Sink 队列拒绝 borrowed response chunk 时，Module 累计 `dropped_bytes` 并输出 `capture.response.body.gap`，不会阻塞代理数据面。
