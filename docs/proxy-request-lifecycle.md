# Proxy request lifecycle

一个逻辑请求拥有一个 `trace_id` 和 request scope；每次 directive 解析与上游访问拥有独立 attempt scope。

```text
Prepare token
  -> open request modules
  -> RequestStarted
  -> request body available/end
  -> attempt N: resolve plan
       -> open attempt modules
       -> AttemptStarted / DirectiveResolved
       -> outbound request + body mutation barriers
       -> UpstreamStarted
       -> upstream response mutation barrier
       -> raw chunks -> transforms -> SSE/JSON projection
       -> body end -> AttemptFinished -> close attempt scope
  -> downstream facts/projection
  -> RequestFinished -> close request scope
```

retry 在收到最终响应头前取消当前 attempt。被取消的 attempt 以 `replaced` 结束，下一次远端 directive 会重新读取和编译；request Module 保留状态，attempt Module 全部重新创建。成功响应不会在响应头处结束 attempt scope，而是在 upstream body EOF/Close 时结束。

请求正文经过 `Content-Length` 验证和 FIFO 内存预算后成为不可变 canonical body。request Module 必须在 `RequestBodyAvailable` 的提交 barrier 内取得 lease，不能异步保留裸指针。

响应流边界：

- `UpstreamBodyChunk`：上游 raw 切片；
- `MutateUpstreamBodyChunk`：有序、提交前流 transform；
- `UpstreamSSEData` / `UpstreamJSONChunk`：transform 后的共享派生投影；
- `DownstreamBodyChunk`：已经写给客户端的字节；
- `DownstreamSSEData` / `DownstreamSSEComment`：下游共享投影。

外部 Record 使用 `schema_version=dproxy.event.v2`，`producer` 是 directive binding ID，`record_id=<trace_id>:<sequence>`，所有 request/attempt Module 共享单调递增 sequence。

Fluent Sink 与 Module runtime 独立。Fluent 关闭时不创建连接、Queue 或 worker，但 Module 仍会编译和执行；没有 Sink 时 emitter 会丢弃 Record 并正确释放 owned data，不影响 mutation。运行阶段 Module panic/错误通过 `/health` 的 `modules` 报告，Sink 失败和队列溢出通过 `event_output` 报告。
