# LLM Usage 插件

`builtin.llmusage` 从受支持的 LLM JSON/SSE 响应中提取供应方报告的 token usage。只有 directive token 在当前 attempt 中声明该插件时才会启用：

```json
{
  "plugins": {
    "llmusage": {
      "protocol": "openai.responses",
      "labels": {"provider": "openai", "account": "primary"}
    }
  }
}
```

## Directive 配置

`protocol` 支持：

- `auto`
- `openai.responses`
- `openai.chat-completions`
- `anthropic.messages`
- `google.generate-content`

`labels` 会原样附加到输出 Record。插件不会根据 endpoint、model 或协议猜测实际供应方和计费身份。

响应 `Content-Type` 决定使用 JSON 或 SSE decoder。非 identity 的 `Content-Encoding` 不会被解析。

## 部署配置

```yaml
- name: llmusage
  type: builtin.llmusage
  llmusage:
    max-sse-metadata-bytes: 0
    max-result-bytes: 0
    max-nesting-depth: 0
```

`0` 表示使用底层库默认值。这些字段只限制 decoder 状态、结果大小和 JSON 嵌套深度，不会自动为请求启用插件。

## 输出事件

- `llm.usage.observed`：规范化 token counters、response ID、model、total source 和保留的 raw usage JSON；
- `llm.usage.not_observed`：响应正常结束但没有报告 usage；
- `llm.usage.failed`：显式启用的响应解析失败或触发资源限制。
