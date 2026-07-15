# Observability plugin architecture

依赖方向保持为：

```text
proxy/retry/response writer
  -> core/observability Signal + Pipeline
       -> plugin/capture
       -> plugin/llmusage
       -> plugin/llmperf
       -> sink/fluent
```

观测插件和 Fluent sink 使用不同接口：前者按 trace 保存解析状态并消费 Signal，后者并发安全地接收不可变 Record。Record 的 `plugin` 字段和健康检查使用配置中的插件实例名称；topic 表示稳定的事件类型。Pipeline 统一负责 Record identity、单 trace sequence、队列资源限制、panic containment、sink 分片和健康聚合。

新增观测插件时：

1. 实现 `observability.Plugin`，每个 trace 返回独立 `TraceObserver`。
2. 如需 directive 配置，再实现 `observability.DirectivePlugin`；配置必须在上游请求前完成严格校验。
3. 不保留 Signal 中的 borrowed body slice；保留 canonical request body 时必须取得 lease。
4. 异步拥有的 buffer 使用 `EmitOwned` 注册 release，由唯一 sink 完成或丢弃后释放。
5. 只通过 `Emitter` 产生 namespaced topic，不自行生成 record ID 或 sequence。
6. payload 解析错误 fail-open；插件 panic 由 Pipeline containment 并降级健康状态。

Fluent sink 约束：

1. 实现并发安全的 `observability.Sink`。
2. 连接与静态配置错误在 `Start` 返回。
3. `Write` 同步消费且不得在返回后保留 Record；不修改 Record，重试和协议 ACK 由输出实现。
4. `Health` 只报告输出自身状态；Pipeline 叠加 queued/dropped 指标。
5. `Close` 应在 context 截止前完成 drain 和资源释放。
