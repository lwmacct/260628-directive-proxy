# Exchange lifecycle

`Exchange` 表示一个入站 HTTP 请求及其完整下游响应；`RoundTrip` 表示其中一次上游访问。一个 Exchange 只有一个 `trace_id`、一份固定的 prepared directive 和一个 exchange-lifetime scope，可以按顺序创建多个 RoundTrip，每个 RoundTrip 拥有独立 round-trip-lifetime scope。

```text
Exchange factory
  └─ Exchange
       ├─ streaming replay store
       ├─ exchange-lifetime Module scope
       ├─ RoundTrip 1 -> round-trip-lifetime Module scope
       ├─ Recovery callback
       ├─ RoundTrip 2 -> round-trip-lifetime Module scope
       └─ downstream response lifecycle
```

Manager 只负责创建 Exchange，并携带 Program runtime 与服务端最大 RoundTrip 上限。项目不再维护活动 Exchange 索引、retry command、tombstone 或任何按 Retry-ID/trace ID 查找请求的控制 API。

## 状态转换

```text
starting_body_stream -> streaming_request -> preparing_round_trip -> awaiting_response
                                              |                    |
                                              | trigger            | trigger
                                              v                    v
                                           recovering <------------+
                                              |
                                              | action=retry
                                              v
                                        retry_requested
                                              |
                                              +----> preparing_round_trip (next RoundTrip)

awaiting_response -> streaming_response -> finished
round-trip/transport/client/recovery failure --> finished
```

- `BeginRoundTrip` 返回强类型 `*RoundTrip`；终态 Exchange、已有活动 RoundTrip 或超出 Recovery budget 时拒绝创建；
- remote directive 在 Exchange 进入 RoundTrip 前解引用一次；`preparing_round_trip` 只打开 round-trip-lifetime scope，并按固定 Plan 构造本次上游请求；
- `unexpected_status`、`response_header_timeout` 或 `transport_error` 可以进入 `recovering`；
- Recovery Controller 的 `retry` 决策先把状态提交为 `retry_requested`，再取消当前 RoundTrip context；
- `forward` 只会在存在尚未提交的异常响应时继续使用该响应；
- `fail` 终止当前请求；
- 收到并接受最终上游响应头后进入 `streaming_response`，不再启动 Recovery；
- round-trip-lifetime scope 在 retry、transport/prepare failure、上游 body EOF/Close 或 Exchange 完成时关闭；exchange-lifetime scope 最后关闭。

每个 Exchange 使用显式 mutex 维护两类顺序：状态、固定 directive 和当前 RoundTrip 属于状态边界；Module scope、投影和下游提交属于生命周期边界。不存在每请求 coordinator goroutine 或控制 channel。

## Recovery budget 与幂等性

Recovery 策略来自已解析 Payload，并受服务端全局限制收紧：

- `max_round_trips` 包含第一次 RoundTrip；
- `max_elapsed` 从 Exchange 开始计时；
- Controller 的 `after_ms` 必须小于剩余时间；
- POST/PATCH 必须在初始请求携带 `Idempotency-Key` 才允许 `retry`；
- 同一触发事件使用确定性的 `event_id=<trace_id>:<round_trip>:<trigger>`，并作为 Controller 请求的 `Idempotency-Key`。

Controller 的 `retry` 使用当前请求已经解析的同一 Payload；远端配置更新只影响之后发起的新请求，不影响当前 Exchange。

## 生命周期与 Module 事件

```text
decode token and resolve canonical Payload
  -> atomically Configure(directive facts + Program + Metadata)
  -> inject trace_id and open exchange modules with Metadata
  -> RequestStarted
  -> DirectivePrepared（一次，包含 source/target/payload digest）
  -> start request body ingest
       -> RequestBodyChunk ... -> RequestBodyEnded
  -> install fixed Recovery budget before first RoundTrip
  -> RoundTrip N: consume the fixed Plan（与 ingest 并行）
       -> open round-trip-lifetime modules
       -> RoundTripStarted（当前活跃的两类 scope，按 Program 全局顺序）
       -> outbound request + streaming body mutation barriers
       -> UpstreamStarted
       -> optional RecoveryStarted before downstream commit
            -> Controller callback -> RecoveryDecided
            -> apply decision -> RecoveryFinished
       -> upstream response mutation barrier
       -> raw chunks -> transforms -> SSE/JSON projection
       -> upstream body end -> RoundTripFinished -> close round-trip-lifetime scope
  -> downstream facts/projection
  -> RequestFinished -> close exchange-lifetime scope
```

Module 通过 `module.Registrar` 声明自己接收的事件和 mutation port；端口值由 `core/lifecycle` 定义。未声明的事件不会投递，未订阅的 SSE/JSON 投影不会创建。例如 `builtin.llmusage` 只接收 upstream response headers、SSE data、JSON chunk 和 body end；`builtin.llmperf` 接收 upstream start、response headers、raw body chunk 和 body end。

exchange-lifetime Module 在正文读取前收到一次 `DirectivePrepared`。每次 Recovery RoundTrip 开始时，exchange-lifetime Module 与新打开的 round-trip-lifetime Module 都收到 `RoundTripStarted`；两类实例按原始 Program 数组位置统一排序。其中 source、target 和 payload digest 来自同一个 prepared directive，不会在 RoundTrip 之间重绑定或报告虚假的变化。metadata 不在各 lifecycle fact 中重复，而是由每次回调的 `module.Context.Metadata` 自动提供。

Recovery 由 `exchange.RecoveryCycle` 持有，使用同一个 `event_id=<trace_id>:<round_trip>:<trigger>` 关联三个只读事实：`RecoveryStarted` 包含 trigger、RoundTrip budget、directive source、Controller module/endpoint/header 和可选的异常响应；`RecoveryDecided` 记录 Controller 返回的 `action` 与 `after_ms`；`RecoveryFinished` 记录实际应用结果，包括 `retry_requested`、`forwarded`、`failed`、Controller/决策错误、预算拒绝或取消。Controller Binding 在 Prepare 阶段编译一次并由同一 Policy 跨 RoundTrip 复用。Controller 请求会携带完整 metadata；Module 回调从 Context 取得同一份 metadata。

## Replay Store

请求正文只由 ingest goroutine 读取一次并追加到 Replay Store。`RequestBodyChunk` 在字节对上游可见前形成提交 barrier；RoundTrip reader 可以读取已保存前缀并在当前尾部等待。

Store 按实际字节限制正文大小，支持未知 `Content-Length`，以内存分段保存小正文并将较大正文 spill 到匿名临时文件。Recovery `retry` 后的新 reader 从 offset 0 重放；最终响应被接受后 Store retire，仍在工作的 reader/ingest 结束后释放存储，不等待长时间下游流结束。

## 响应提交边界

响应流端口：

- `UpstreamBodyChunk`：上游 raw 切片；
- `MutateUpstreamBodyChunk`：有序、提交前 transform；
- `UpstreamSSEData` / `UpstreamJSONChunk`：transform 后的共享派生投影；
- `DownstreamBodyChunk`：已经写给客户端的字节；
- `DownstreamSSEData` / `DownstreamSSEComment`：下游共享投影。

Recovery 只允许发生在最终响应头提交给下游之前。异常状态的捕获正文会被重新组装，所以 `forward` 可以保持原响应；一旦进入 `streaming_response`，SSE 或普通流式正文都不会被透明替换、拼接或重连。

异步 `scope_end` lane 在 scope 结束前必须 drain，即使客户端 context 已取消；Finish cause 仍会标记为 `completed`、`failed`、`canceled` 或 `replaced`。外部 Record 使用 `schema_version=dp.event.v4`，顶层自动携带完整 metadata；同一 Exchange 的两类 Module 共享单调递增 sequence。

Fluent Sink 与 Program runtime 独立。Fluent 关闭时不创建连接、Queue 或 worker，但 Module 仍会编译和执行。运行阶段 Module panic/错误通过 `/health.modules` 报告，Sink 失败和队列溢出通过 `/health.event_output` 报告。
