# Capture Module

`builtin.capture` 是 request-scope Module，跨越全部 retry attempt，记录请求、响应和生命周期审计事件。

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

Capture 订阅 request/attempt facts、downstream raw body、共享 SSE data/comment 投影和 request finish。请求正文端口使用 `ordered_lane + before_commit`，在正文释放前取得 lease；其余端口异步执行并在 request scope 结束前 drain。

主要 topics 为 `capture.request.*`、`capture.directive.*`、`capture.attempt.*`、`capture.retry.*` 和 `capture.response.*`。正文 chunk 使用 MessagePack binary，包含 offset、length 和 chunk index。Sink 队列拒绝 borrowed response chunk 时，Module 累计 `dropped_bytes` 并输出 `capture.response.body.gap`，不会阻塞代理数据面。
