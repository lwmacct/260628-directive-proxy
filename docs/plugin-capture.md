# Capture 插件

`builtin.capture` 记录请求和响应的审计事件。只有 directive token 在当前 attempt 中声明该插件时才会启用：

```json
{
  "plugins": {
    "capture": {}
  }
}
```

## 部署配置

部署配置负责注册插件，并设置资源限制和脱敏规则：

```yaml
- name: capture
  type: builtin.capture
  capture:
    body-chunk-bytes: 32768
    max-sse-event-bytes: 1048576
    redact-headers: [authorization, proxy-authorization, cookie, set-cookie, x-api-key, api-key]
    redact-query: [access_token, api_key, apikey, key, token]
```

- `body-chunk-bytes`：单条请求或响应正文 Record 的最大字节数；
- `max-sse-event-bytes`：单个 SSE 语义事件的解析上限；
- `redact-headers`：需要脱敏的 HTTP header 名称或大小写不敏感 glob；
- `redact-query`：需要脱敏的 URL query 参数名称或大小写不敏感 glob。

响应 Capture 的共享内存预算和溢出策略统一配置在 `observability.response-capture-memory`。

## 输出事件

插件产生 `capture.**` topics，包括请求 header、Metadata、正文 chunk/end、directive 解析、attempt、重试请求、响应 header/body/SSE 事件和请求完成状态。

正文 chunk 使用 MessagePack binary 数据，并包含 offset、length 和 chunk index。Header 和 URL 在产生 Record 前完成脱敏。完整事件契约见 [Proxy request lifecycle](proxy-request-lifecycle.md)。
