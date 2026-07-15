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

观测插件和 Fluent sink 使用不同接口：前者按 trace 保存解析状态并消费 Signal，后者按 Pipeline 分配的 shard 同步消费不可变 Record。Record 的 `plugin` 字段和健康检查使用固定内置名称（例如 `builtin.capture`）；topic 表示稳定的事件类型。Pipeline 统一负责 Record identity、单 trace sequence、唯一队列资源限制、panic containment、sink 分片和健康聚合。

`observability.fluent.enabled` 是整个子系统的总开关。关闭时不实例化插件、Trace、Queue、worker 或 Fluent client；directive 中的 `plugins` 保留 JSON 与大小边界检查，但不做注册或 spec 校验，也不影响代理数据面。健康状态报告为 `disabled`，服务整体仍为 `ok`。

新增观测插件时：

1. 实现 `observability.Plugin`，每个 trace 返回独立 `TraceObserver`。
2. 实现 `observability.DirectivePlugin.ConfigureSpec`，由 directive spec 构造 attempt 级、不可变的已配置 Plugin；全部可调参数必须属于 spec，并在访问上游前严格校验。
3. 不保留 Signal 中的 borrowed body slice；响应正文使用 `EmitBorrowed`，由 Pipeline 在 Queue admission 成功后复制；保留 canonical request body 时必须取得 lease。
4. 异步拥有的 buffer 使用 `EmitOwned` 注册 release，由唯一 sink 完成或丢弃后释放。
5. 只通过 `Emitter` 产生 namespaced topic，不自行生成 record ID 或 sequence。
6. payload 解析错误 fail-open；插件 panic 由 Pipeline containment 并降级健康状态。

部署配置不包含插件列表或插件参数。Fluent 开启时程序固定注册全部内置插件；directive 未声明的插件不会创建 observer，也不会消费 Signal。

Fluent sink 约束：

1. 实现 `observability.Sink`；Pipeline 的每个 shard 固定绑定同索引 Fluent client，`connections` 同时决定连接数和发送并行度。
2. 连接与静态配置错误在 `Start` 返回。
3. `Write` 同步消费且不得在返回后保留 Record；不修改 Record，重试和协议 ACK 由输出实现。
4. `Health` 只报告输出自身状态；Pipeline 叠加 queued/dropped 指标。
5. `Close` 应在 context 截止前完成 drain 和资源释放。
6. Fluent client 内部 queue 是实现细节；部署侧只配置 Pipeline 的全局 `max-records` 和 `max-bytes`。
