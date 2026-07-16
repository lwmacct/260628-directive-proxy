# Exchange lifecycle

`Exchange` 表示一个入站 HTTP 请求及其完整下游响应交换；`Attempt` 表示其中一次 directive 解析和上游访问。一个 Exchange 只有一个 `trace_id` 和一个 request scope，可以按顺序创建多个 Attempt，每个 Attempt 拥有独立 attempt scope。

```text
Manager
  ├─ active index / retry command / tombstone
  └─ Exchange
       ├─ streaming replay store
       ├─ request Module scope
       ├─ Attempt 1 -> attempt Module scope
       ├─ Attempt 2 -> attempt Module scope
       └─ downstream response lifecycle
```

`Manager` 不拥有单个请求的状态机。生命周期方法都落在 `Exchange` 或其返回的 `*Attempt` 上；Manager 仅为管理 API 提供活动快照和带 CAS 前置条件的 retry command。

## 状态转换

```text
starting_body_stream -> streaming_request -> resolving_directive -> awaiting_response
                                              ^                    |
                                              |                    | retry accepted
                                              +-- retry_requested <-+

awaiting_response -> streaming_response -> finished
resolve/transport/client failure ----------> finished
```

- `BeginAttempt` 返回强类型 `*Attempt`；终态 Exchange、活动 Attempt 或超过 `max-attempts` 时拒绝创建；
- retry 只在 `awaiting_response` 接受，并以当前 Attempt 作为 CAS 条件；
- retry command 先把状态提交为 `retry_requested`，再取消上游 context；并发重复命令返回同一个结果，只执行一次 cancel；
- 收到最终上游响应头后，Exchange 从 Manager 的可重试索引移除，但继续拥有 upstream body、downstream body 和 request scope，直到 `Complete`；
- attempt scope 在 retry、transport error、upstream body EOF/Close 或 Exchange 完成时关闭；request scope 最后关闭。

每个 Exchange 使用显式 mutex 维护两类顺序：状态、当前 Attempt、metadata 和 retry result 属于状态边界；Module scope、投影和下游提交属于生命周期边界。不存在每请求 coordinator goroutine 或 channel。

## 生命周期与 Module 事件

```text
prepare directive
  -> open request modules
  -> RequestStarted
  -> start request body ingest
       -> RequestBodyChunk ... -> RequestBodyEnded
  -> Attempt N: resolve directive plan（与 ingest 并行）
       -> open attempt modules
       -> AttemptStarted / DirectiveResolved
       -> outbound request + streaming body mutation barriers
       -> UpstreamStarted
       -> upstream response mutation barrier
       -> raw chunks -> transforms -> SSE/JSON projection
       -> upstream body end -> AttemptFinished -> close attempt scope
  -> downstream facts/projection
  -> RequestFinished -> close request scope
```

Module 通过 `Binder` 声明自己接收的事件和 mutation port。未声明的事件不会投递，未订阅的 SSE/JSON 投影不会创建。例如 `builtin.llmusage` 只接收 upstream response headers、SSE data、JSON chunk 和 body end；`builtin.llmperf` 接收 upstream start、response headers、raw body chunk 和 body end。

请求正文只由 ingest goroutine 读取一次并追加到 Replay Store。`RequestBodyChunk` 在字节对上游可见前形成提交 barrier；Attempt reader 可以读取已保存前缀并在当前尾部等待。Store 按实际字节限制正文大小，支持未知 `Content-Length`，以内存分段保存小正文并将较大正文 spill 到匿名临时文件。最终响应头关闭重试窗口后立即 retire Store；仍在工作的 reader/ingest 结束后释放存储，不等待下游响应结束。

响应流边界：

- `UpstreamBodyChunk`：上游 raw 切片；
- `MutateUpstreamBodyChunk`：有序、提交前流 transform；
- `UpstreamSSEData` / `UpstreamJSONChunk`：transform 后的共享派生投影；
- `DownstreamBodyChunk`：已经写给客户端的字节；
- `DownstreamSSEData` / `DownstreamSSEComment`：下游共享投影。

异步 `scope_end` lane 在 scope 结束前必须 drain，即使客户端 context 已取消；Finish cause 仍会准确标记为 `completed`、`failed`、`canceled` 或 `replaced`。外部 Record 使用 `schema_version=dproxy.event.v2`，同一 Exchange 的 request/attempt Module 共享单调递增 sequence。

Fluent Sink 与 Module runtime 独立。Fluent 关闭时不创建连接、Queue 或 worker，但 Module 仍会编译和执行；没有 Sink 时 emitter 丢弃 Record 并正确释放 owned data，不影响 mutation。运行阶段 Module panic/错误通过 `/health` 的 `modules` 报告，Sink 失败和队列溢出通过 `event_output` 报告。
