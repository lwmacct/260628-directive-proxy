# LLM Usage Module

`builtin.llmusage` 是 attempt-scope Module，从 LLM JSON/SSE 响应中提取供应方报告的 token usage。

```json
{
  "program": [
    {
      "scope": "attempt",
      "id": "usage",
      "module": "builtin.llmusage",
      "config": {
        "protocol": "openai.responses",
        "labels": {"provider":"openai"},
        "max-sse-metadata-bytes": 0,
        "max-result-bytes": 0,
        "max-nesting-depth": 0
      }
    }
  ]
}
```

`protocol` 支持 `auto`、`openai.responses`、`openai.chat-completions`、`anthropic.messages` 和 `google.generate-content`。资源上限的 `0` 使用底层库默认值；代理允许的最大值分别为 1 MiB、16 MiB 和 256。

该 Module 明确只订阅：

- upstream response headers；
- `UpstreamSSEData`；
- `UpstreamJSONChunk`；
- upstream body end。

它不订阅 raw upstream chunk。SSE 解析由当前活跃的 exchange/attempt scope 共享投影完成，Module 只接收语义 data event。处理运行在 ordered async lane，并在 attempt scope 结束前 drain。

输出 topics：`llm.usage.observed`、`llm.usage.not_observed`、`llm.usage.failed`。
