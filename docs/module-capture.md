# Capture Module

`builtin.capture` 是 request-scope Module，跨越全部 Recovery Attempt，记录请求、响应和生命周期审计事件。

```json
{
  "program": {
    "request": [{
      "id": "capture",
      "module": "builtin.capture",
      "config": {
        "body-chunk-bytes": 32768,
        "redact-headers": ["authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key"],
        "redact-query": ["access_token", "api_key", "apikey", "key", "token"]
      }
    }]
  }
}
```

配置：

- `body-chunk-bytes`：单条正文 Record 的最大字节数；`0` 使用 32 KiB，上限 1 MiB；
- `redact-headers`：需要脱敏的 header 精确名称或大小写不敏感 glob；
- `redact-query`：需要脱敏的 URL query 参数名称或 glob。

Capture 订阅 request/attempt facts、流式 `RequestBodyChunk`、downstream raw body、共享 SSE data/comment 投影和 request finish。请求正文端口使用 `ordered_lane + before_commit`，Capture 按 `body-chunk-bytes` 重新分片并以 borrowed Record 提交，Dispatcher 在入队时复制自己拥有的数据；其余端口异步执行并在 request scope 结束前 drain。

主要 topics 为 `capture.request.*`、`capture.directive.*`、`capture.attempt.*`、`capture.retry.*` 和 `capture.response.*`。其中 `capture.retry.*` 表示 Recovery Controller 触发的内部 Attempt 切换，不对应外部 Retry API。正文 chunk 使用 MessagePack binary，包含 offset、length 和 chunk index。Sink 队列拒绝 borrowed response chunk 时，Module 累计 `dropped_bytes` 并输出 `capture.response.body.gap`，不会阻塞代理数据面。
