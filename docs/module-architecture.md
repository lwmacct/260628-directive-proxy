# Directive Module architecture

Module 是 directive 驱动的请求生命周期扩展单元，不局限于可观测性。Module 可以观察类型化事件，也可以在数据提交前修改请求、响应或流切片。生命周期由 Exchange 拥有，Program runtime 只执行已经编译的扩展程序。

```text
program.Program（Payload 中的有序 Spec）
  -> program.Runtime.Compile（每个已解析 Payload 一次）
program.Executable（不可变 Binding 列表）
  -> program.Run（每个 Exchange）
  -> request / attempt Scope
module.Binding.Open
  -> module.Instance.Bind(module.Registrar)
core/lifecycle 类型化 ports
```

directive 使用有序数组声明程序：

```json
{
  "program": {
    "request": [
      {"id":"capture","module":"builtin.capture","config":{}}
    ],
    "attempt": [
      {"id":"usage","module":"builtin.llmusage","config":{"protocol":"openai.responses"}}
    ]
  }
}
```

`id` 在同一数组内唯一，并作为 Record 的 `producer`；`module` 选择进程级 Definition。数组顺序就是 mutation 和事件提交顺序，不使用 map，也不存在隐式优先级。

## 与 Exchange 生命周期的关系

四个 core package 的职责单向依赖，不互相替代：

- `core/lifecycle` 只定义 Fact、Stream、Draft 和 Outcome，不拥有状态或调度；
- `core/module` 只定义 Definition、Binding、Instance、Registrar、Policy 和执行 Context 等 Module SPI；
- `core/program` 注册 Definition、编译 Program、创建 Run/Scope、执行 handler、投影流并汇总健康；
- `core/exchange` 是生命周期唯一拥有者，驱动 request、attempt、recovery 和 downstream 状态转换；
- `Manager` 是轻量 Exchange factory，只携带 Program runtime 和服务端 Attempt 上限，不维护活动索引或外部 command；
- `Exchange` 拥有入站请求生命周期、request scope、下游响应和当前 Attempt；流式 Replay Store 通过请求 context 交给 RecoveryTransport；
- `Attempt` 是 Exchange 创建的强类型子对象，拥有一次 directive 解析、上游访问和 attempt scope；
- `program.Executable` 在 Prepare 阶段生成一次；Recovery Attempt 复用同一批不可变 Binding，只重新调用 `Binding.Open` 创建实例。

生命周期方法直接接收 `*Attempt`，不传递裸 attempt 整数，也没有每请求 coordinator goroutine/channel。状态转换与 Module 事件提交分别由明确的 mutex 串行化。

## Scope

- request scope 由 Exchange 在读取请求正文前打开，跨越全部 Recovery Attempt 和下游响应；
- attempt scope 在每次 directive plan 解析后打开，Recovery retry、transport error 或响应 body 结束时关闭；
- request Module 可以观察所有 attempt，attempt Module 不会泄漏状态到下一次重试；
- scope 结束时先 drain `scope_end` lane，再调用 Instance `Finish`；客户端取消只改变 Finish cause，不跳过 drain。

## 类型化端口

- Hook：提交前可变 Draft，例如 outbound request/body chunk、upstream response/body chunk；
- Transform：按 directive 顺序修改流数据；
- Stream：只读数据或派生投影，例如 raw chunk、JSON chunk、SSE data/comment；
- Fact：不可变生命周期事实，例如 request started、directive resolved、Recovery transaction、body ended；
- Command：预留给异步影响未来状态的控制消息，不允许异步任务反向修改已经提交的数据。

Module 通过 `module.Registrar` 明确声明端口，端口值来自 `core/lifecycle`。未订阅的投影不会创建。例如 `builtin.llmusage` 只订阅 response headers、`UpstreamSSEData`、`UpstreamJSONChunk` 和 upstream end，不接收通用 raw Signal。

Recovery callback 是一等、只读的三阶段事务端口：`OnRecoveryStarted` 在调用 Controller 前投递，`OnRecoveryDecided` 在收到合法决策后投递，`OnRecoveryFinished` 在决策实际应用或失败后且在 `AttemptFinished` 前投递。三个事件共享同一 `EventID`。`module.Context.Sequence` 是同一 Exchange Run 内单调递增的生命周期序号；Recovery 事件的 `module.Context.EventID` 与 payload 的 `EventID` 相同。Module 可以完整观察 trigger、Controller 请求上下文、决策和最终 outcome，但不能覆盖 directive 或 Controller 决策。

## 执行策略

执行策略属于每个 binding，而不是整个 Module：

- `executor=caller`：在调用线程执行；
- `executor=ordered_lane`：进入该 Instance 的有序异步 lane；
- `barrier=before_commit`：必须等待此前 lane 工作和当前回调完成后才能提交数据；
- `barrier=scope_end`：请求路径可继续，scope 结束前必须 drain；
- `barrier=none`：为未来独立 Command executor 预留；当前 scope scheduler 仍会在结束时 drain，内置 Module 不使用它；
- overflow 支持 `block`、`drop`、`fail_request`。

所有 mutation 自动形成 `before_commit` barrier。异步任务只能观察已经拥有的数据，或产生未来 Command；不能修改已经写给上游/下游的字节。

## 投影、Emitter 与 Event Dispatcher

每个方向只构造一次共享投影：

```text
upstream raw
├─ raw subscribers
├─ ordered body transforms
├─ SSE / JSON projection（按订阅按需创建）
└─ downstream committed bytes
```

`core/program.Runtime` 负责 Definition registry、Program 编译、Run、Scope 和 Module 健康。Module 通过 `module.Context.Emitter` 产生可选外部 Record；Runtime 只依赖 `core/event.Provider`。

`core/event.Dispatcher` 实现该 provider，负责 `dproxy.event.v2` Record、单 trace sequence、buffer ownership、有界队列、分片和 Sink。Record 包含 `producer`、`topic`、`record_id`、`trace_id`、`attempt`、`sequence`、`occurred_at` 和 `data`。

`server.fluent.enabled=false` 时不创建 Dispatcher、Sink、Queue 或连接；Program runtime 使用 discard emitter，Module 仍注册、编译和执行，因此修改型 Module 不依赖事件输出。

Module panic 或回调错误会隔离该 Instance、使 barrier 失败，并将对应 Definition 的健康状态标为 degraded。`/health.modules` 与 `/health.event_output` 分别反映两个独立子系统。
