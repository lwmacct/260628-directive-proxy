# Capture 插件

`builtin.capture` 记录请求和响应的审计事件。只有 directive token 在当前 attempt 中声明该插件时才会启用：

```json
{
  "plugins": {
    "capture": {
      "body-chunk-bytes": 32768,
      "max-sse-event-bytes": 1048576,
      "redact-headers": ["authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key"],
      "redact-query": ["access_token", "api_key", "apikey", "key", "token"]
    }
  }
}
```

## Directive 配置

- `body-chunk-bytes`：单条请求或响应正文 Record 的最大字节数，`0` 使用 32 KiB 默认值，上限 1 MiB；
- `max-sse-event-bytes`：单个 SSE 语义事件的解析上限，`0` 使用 1 MiB 默认值，上限 16 MiB；
- `redact-headers`：需要脱敏的 HTTP header 名称或大小写不敏感 glob；
- `redact-query`：需要脱敏的 URL query 参数名称或大小写不敏感 glob。

字段省略时使用插件内置的安全默认值；所有可调参数都属于 directive spec，不存在部署级 Capture 配置。未知字段、非法 glob、重复 pattern 或超出资源上限的值会在访问上游前拒绝。

响应 body slice 使用 borrowed emission：Pipeline 仅在 Fluent Queue 接收 Record 后复制正文数据。Queue 满时当前 chunk 非阻塞丢弃，插件累计 `dropped_bytes` 并尝试输出 `capture.response.body.gap`；Capture 不具有独立内存预算，也不会对代理响应施加 backpressure。

## 输出事件

插件产生 `capture.**` topics，包括请求 header、Metadata、正文 chunk/end、directive 解析、attempt、重试请求、响应 header/body/SSE 事件和请求完成状态。

正文 chunk 使用 MessagePack binary 数据，并包含 offset、length 和 chunk index。Header 和 URL 在产生 Record 前完成脱敏。完整事件契约见 [Proxy request lifecycle](proxy-request-lifecycle.md)。
