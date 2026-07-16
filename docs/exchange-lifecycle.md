# Exchange lifecycle

`Exchange` 表示一个入站 HTTP 请求及其完整下游响应；`Attempt` 表示其中一次 directive 解析和上游访问。一个 Exchange 只有一个 `trace_id` 和一个 request scope，可以按顺序创建多个 Attempt，每个 Attempt 拥有独立 attempt scope。

```text
Exchange factory
  └─ Exchange
       ├─ streaming replay store
       ├─ request Module scope
       ├─ Attempt 1 -> attempt Module scope
       ├─ Recovery callback
       ├─ Attempt 2 -> attempt Module scope
       └─ downstream response lifecycle
```

Manager 只负责创建 Exchange，并携带 Module runtime 与服务端最大 Attempt 上限。项目不再维护活动 Exchange 索引、retry command、tombstone 或任何按 Retry-ID/trace ID 查找请求的控制 API。

## 状态转换

```text
starting_body_stream -> streaming_request -> resolving_directive -> awaiting_response
                                              |                    |
                                              | trigger            | trigger
                                              v                    v
                                           recovering <------------+
                                              |
                                              | action=retry
                                              v
                                        retry_requested
                                              |
                                              +----> resolving_directive (next Attempt)

awaiting_response -> streaming_response -> finished
resolve/transport/client/recovery failure --> finished
```

- `BeginAttempt` 返回强类型 `*Attempt`；终态 Exchange、已有活动 Attempt 或超出 Recovery budget 时拒绝创建；
- remote directive 在每个 Attempt 的 `resolving_directive` 阶段重新读取和编译；
- `unexpected_status`、`response_header_timeout`、`transport_error` 或可选的 `directive_error` 可以进入 `recovering`；
- Recovery Controller 的 `retry` 决策先把状态提交为 `retry_requested`，再取消当前 Attempt context；
- `forward` 只会在存在尚未提交的异常响应时继续使用该响应；
- `fail` 终止当前请求；
- 收到并接受最终上游响应头后进入 `streaming_response`，不再启动 Recovery；
- attempt scope 在 retry、transport/resolve failure、上游 body EOF/Close 或 Exchange 完成时关闭；request scope 最后关闭。

每个 Exchange 使用显式 mutex 维护两类顺序：状态、当前 Attempt 和 metadata 属于状态边界；Module scope、投影和下游提交属于生命周期边界。不存在每请求 coordinator goroutine 或控制 channel。

## Recovery budget 与幂等性

Recovery 策略来自稳定 token envelope，并受服务端全局限制收紧：

- `max_attempts` 包含第一次 Attempt；
- `max_elapsed` 从 Exchange 开始计时；
- Controller 的 `after_ms` 必须小于剩余时间；
- POST/PATCH 必须在初始请求携带 `Idempotency-Key` 才允许 `retry`；
- 同一触发事件使用确定性的 `event_id=<trace_id>:<attempt>:<trigger>`，并作为 Controller 请求的 `Idempotency-Key`。

Controller 必须在返回 `retry` 前完成远程 directive 修改。下一 Attempt 会重新读取远端 Payload，因此不需要额外 retry identity、更新 API 或跨进程活动请求路由。

## 生命周期与 Module 事件

```text
prepare stable token envelope
  -> open request modules
  -> RequestStarted
  -> start request body ingest
       -> RequestBodyChunk ... -> RequestBodyEnded
  -> Attempt N: resolve directive plan（与 ingest 并行）
       -> open attempt modules
       -> AttemptStarted / DirectiveResolved
       -> outbound request + streaming body mutation barriers
       -> UpstreamStarted
       -> optional Recovery callback before downstream commit
       -> upstream response mutation barrier
       -> raw chunks -> transforms -> SSE/JSON projection
       -> upstream body end -> AttemptFinished -> close attempt scope
  -> downstream facts/projection
  -> RequestFinished -> close request scope
```

Module 通过 `Binder` 声明自己接收的事件和 mutation port。未声明的事件不会投递，未订阅的 SSE/JSON 投影不会创建。例如 `builtin.llmusage` 只接收 upstream response headers、SSE data、JSON chunk 和 body end；`builtin.llmperf` 接收 upstream start、response headers、raw body chunk 和 body end。

`RetryRequested` 仍是内部生命周期事实，表示 Recovery Controller 已决定切换到下一 Attempt；它不对应外部 Retry API 或 Retry-ID。

## Replay Store

请求正文只由 ingest goroutine 读取一次并追加到 Replay Store。`RequestBodyChunk` 在字节对上游可见前形成提交 barrier；Attempt reader 可以读取已保存前缀并在当前尾部等待。

Store 按实际字节限制正文大小，支持未知 `Content-Length`，以内存分段保存小正文并将较大正文 spill 到匿名临时文件。Recovery `retry` 后的新 reader 从 offset 0 重放；最终响应被接受后 Store retire，仍在工作的 reader/ingest 结束后释放存储，不等待长时间下游流结束。

## 响应提交边界

响应流端口：

- `UpstreamBodyChunk`：上游 raw 切片；
- `MutateUpstreamBodyChunk`：有序、提交前 transform；
- `UpstreamSSEData` / `UpstreamJSONChunk`：transform 后的共享派生投影；
- `DownstreamBodyChunk`：已经写给客户端的字节；
- `DownstreamSSEData` / `DownstreamSSEComment`：下游共享投影。

Recovery 只允许发生在最终响应头提交给下游之前。异常状态的捕获正文会被重新组装，所以 `forward` 可以保持原响应；一旦进入 `streaming_response`，SSE 或普通流式正文都不会被透明替换、拼接或重连。

异步 `scope_end` lane 在 scope 结束前必须 drain，即使客户端 context 已取消；Finish cause 仍会标记为 `completed`、`failed`、`canceled` 或 `replaced`。外部 Record 使用 `schema_version=dproxy.event.v2`，同一 Exchange 的 request/attempt Module 共享单调递增 sequence。

Fluent Sink 与 Module runtime 独立。Fluent 关闭时不创建连接、Queue 或 worker，但 Module 仍会编译和执行。运行阶段 Module panic/错误通过 `/health.modules` 报告，Sink 失败和队列溢出通过 `/health.event_output` 报告。
