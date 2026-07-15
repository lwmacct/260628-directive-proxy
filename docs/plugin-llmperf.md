# LLM Performance 插件

`builtin.llmperf` 使用 `github.com/lwmacct/260714-go-pkg-llmperf` 测量响应时间线。只有 directive token 在当前 attempt 中声明该插件时才会启用：

```json
{
  "plugins": {
    "llmperf": {
      "protocol": "openai.responses",
      "labels": {"provider": "openai"}
    }
  }
}
```

## Directive 配置

`protocol` 支持：

- `auto`，仅用于 SSE；
- `openai.responses`；
- `openai.chat-completions`；
- `anthropic.messages`；
- `google.generate-content`。

`labels` 会附加到最终性能 Record。

计时从上游 attempt 开始，以代理实际观测响应 header 和 body chunk 的时间为准。JSON 响应只能提供 transport 指标；SSE 响应还可提供 first byte、first output、first visible text 和 generation completion 等语义指标。

当前插件没有从其他插件共享 token count，因此依赖 token 数量的 TPOT/TPS 指标会按底层库语义标记为不可用，不会伪造为 `0`。

## 部署配置

```yaml
- name: llmperf
  type: builtin.llmperf
  llmperf:
    max-sse-metadata-bytes: 0
    max-retained-bytes: 0
    max-nesting-depth: 0
```

`0` 表示使用底层库默认值。这些字段只限制 decoder 内存和 JSON 嵌套深度，不会自动为请求启用插件。

## 输出事件

- `llm.perf.first_byte`；
- `llm.perf.first_output`；
- `llm.perf.first_text`；
- `llm.perf.generation_completed`；
- `llm.perf.observed`：最终 timeline、outcome、terminal state 和派生指标；
- `llm.perf.failed`：显式的解析或生命周期错误。
