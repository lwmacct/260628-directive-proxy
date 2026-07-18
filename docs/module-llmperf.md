# LLM Performance Module

`builtin.llmperf` 是 round-trip-lifetime Module，测量上游请求、响应头、首字节和语义输出时间线。

```json
{
  "metadata": {"provider": "openai"},
  "modules": [
    {
      "module": "builtin.llmperf",
      "config": {
        "protocol": "openai.responses",
        "max-sse-metadata-bytes": 0,
        "max-retained-bytes": 0,
        "max-nesting-depth": 0
      }
    }
  ]
}
```

公共业务维度使用 Payload 顶层 `metadata`；它会由运行时传入 Module Context，并出现在每条 `dp.event.v6` Record 的顶层，不在 Module config 或 topic data 中重复定义。

该 Module 订阅 upstream started、response headers、raw upstream body chunk 和 body end。raw 端口保留代理实际读取切片的时间戳；处理进入 ordered async lane，并在 round-trip lifetime 结束前 drain。

`protocol` 支持 `auto`（仅 SSE）、`openai.responses`、`openai.chat-completions`、`anthropic.messages` 和 `google.generate-content`。主要 topics 为 `llm.perf.first_byte`、`llm.perf.first_output`、`llm.perf.first_text`、`llm.perf.generation_completed`、`llm.perf.observed` 和 `llm.perf.failed`。
