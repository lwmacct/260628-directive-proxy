# LLM Observation Module

`llmobserve` 是 round-trip-lifetime Module。它在同一个增量解码器中完成 SSE framing、严格选择性 JSON 扫描、token usage 提取和响应性能测量，替代原先相互独立的 usage/performance Module。

```json
{
  "metadata": {"provider": "openai"},
  "modules": [
    {
      "module": "llmobserve",
      "config": {
        "protocol": "auto",
        "observe": ["usage", "performance"],
        "limits": {
          "max-sse-metadata-bytes": 65536,
          "max-retained-bytes": 65536,
          "max-nesting-depth": 128,
          "max-object-members": 4096,
          "max-key-bytes": 4096
        }
      }
    }
  ]
}
```

公共业务维度使用 Payload 顶层 `metadata`；运行时会把它传入 Module Context，并放在每条 `dp.event.v6` Record 顶层，Module topic data 不重复这些字段。

## 配置

- `protocol`：`auto`、`openai.responses`、`openai.chat-completions`、`anthropic.messages` 或 `google.generate-content`。`auto` 只适用于具有强协议标识的 SSE；JSON 响应必须明确声明协议。
- `observe`：必填、不可为空且不可重复；可选择 `usage`、`performance` 或两者。
- `limits`：全部可选；`0` 使用底层库默认值。

默认限制依次为 64 KiB SSE metadata、64 KiB retained selected values、128 层 JSON 嵌套、4096 个 object members 和 4096 bytes encoded key。代理接受的上限依次为 1 MiB、16 MiB、256、65536 和 1 MiB。

解码器拒绝非法 UTF-8、错误的 escape/surrogate、重复 object key、非法 number、尾随逗号、多份 JSON value 和截断文档。大型未选择 output 字符串会被完整校验但不保留；`max-retained-bytes` 只约束身份、usage 等选中值，因此 completed event 即使重复数 MiB output 也不需要缓存完整 event。

## 生命周期与输出

Module 只订阅 `UpstreamStarted`、`UpstreamResponseStarted`、原始 `UpstreamBodyChunk` 和 `UpstreamBodyEnded`。每个 Recovery RoundTrip 打开独立实例；同一响应只进行一次 SSE/JSON 增量解析，不依赖 core 的上游语义投影。

只观察 2xx、`application/json`、`application/*+json` 或 `text/event-stream` 响应。上游正文必须是 identity encoding；代理会请求 identity，若上游仍返回压缩正文则输出失败事件。

输出 topics：

- `llm.response.milestone`：增量输出 `first_byte`、`first_output`、`first_text` 和 `generation_completed`，包含时间、序号、output kind 与时间精度；
- `llm.response.usage`：每份增量或最终 usage snapshot，包含规范化 token 计数、response/model 身份、total 来源和原始 usage JSON；
- `llm.response.observed`：response 结束后的统一结果，包含 protocol、format、outcome、terminal、全部 usage、milestones 和派生 metrics；
- `llm.response.failed`：content type/encoding、decoder feed 或 finish 失败，包含失败阶段与底层错误。

SSE 可报告语义首输出、首文本、生成完成时间和生成速率；普通 JSON 没有流式语义边界，因此只提供 response header、first byte、end-to-end 等 transport metrics，并明确标记其余指标不可用。
