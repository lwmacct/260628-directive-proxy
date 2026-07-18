# Directive Module architecture

Module 是 directive 驱动的请求生命周期扩展单元，不局限于可观测性。Module 可以观察类型化事件，也可以在数据提交前修改请求、响应或流切片。生命周期由 Exchange 拥有，Program runtime 只执行已经编译的扩展程序。

```text
module.Specs（Payload.modules 中的有序 Module Spec）
  -> program.Runtime.Compile（每个已解析 Payload 一次）
program.Executable（不可变 Binding 列表）
  -> program.Run（每个 Exchange）
  -> exchange / round-trip lifetime Scope
module.Binding.Open
  -> module.Instance.Bind(module.Registrar)
core/lifecycle 类型化 ports
```

directive 使用有序数组声明程序：

```json
{
  "modules": [
    {"module":"builtin.capture","config":{}},
    {"module":"builtin.llmusage","config":{"protocol":"openai.responses"}}
  ]
}
```

`modules` 每项只包含 `module` 和可选 `config`，对应唯一的 `module.Spec`。所有 Definition 注册到同一个进程级 Module Catalog，名称全局唯一；Program Compiler 要求目标 Definition 提供 Program capability，并把 Module 名直接作为 Record 的 `producer`。Program Definition 通过 `Lifetime()` 静态声明 `exchange` 或 `round_trip`；directive 不再覆盖生命周期。数组顺序是所有当前活跃 Module 的全局 mutation 和事件提交顺序。

`recovery.controller` 不是 Module。它直接声明控制面 HTTP callback 的 `url`、可选 `headers` 和可选 `timeout`，并在 Prepare 阶段由 `adapter/recoveryhttp` 编译为不可变 Binding；Policy 只保存 Binding 并跨全部 RoundTrip 复用。Recovery HTTP compiler 不注册到 Module Catalog，RecoveryTransport 也不解释 Controller 参数。

## 与 Exchange 生命周期的关系

相关 core package 的职责单向依赖，不互相替代：

- `core/lifecycle` 只定义 Fact、Stream、Draft 和 Outcome，不拥有状态或调度；
- `core/module` 定义 Program Module Spec、Definition Catalog，以及 Binding、Instance、Registrar、Policy 和执行 Context；
- `core/program` 从 Catalog 选择 Program Definition，编译 Program、创建 Run/Scope、执行 handler、投影流并汇总健康；
- `core/recovery` 定义类型化 Controller Spec、决策 Binding、Recovery Policy 和 callback 协议；`adapter/recoveryhttp` 编译并执行具体 HTTP callback；
- `core/exchange` 是生命周期唯一拥有者，驱动 request、round trip、recovery 和 downstream 状态转换；
- `Manager` 是轻量 Exchange factory，只携带 Program runtime 和服务端 RoundTrip 上限，不维护活动索引或外部 command；
- `Exchange` 拥有入站请求生命周期、exchange-lifetime scope、下游响应和当前 RoundTrip；流式 Replay Store 通过请求 context 交给 RecoveryTransport；
- `RoundTrip` 是 Exchange 创建的强类型子对象，拥有一次上游访问和 round-trip-lifetime scope；
- `program.Executable` 在 Prepare 阶段生成一次；Recovery RoundTrip 复用同一批不可变 Binding，只重新调用 `Binding.Open` 创建实例。

`proxy.PreparedDirective` 是 Prepare 阶段唯一的产物，固定持有 `Source + Plan + Program + Recovery + Metadata`。其中 Plan 只描述 target、proxy 和 header policy，Recovery 与 Metadata 独立归属；Transport 在 RoundTrip 循环外读取一次这些值，Exchange 再以单次 `Configure` 原子安装 directive、Program 和 metadata。

生命周期方法直接接收 `*RoundTrip`，不传递裸 round-trip 整数，也没有每请求 coordinator goroutine/channel。状态转换与 Module 事件提交分别由明确的 mutex 串行化。

## Lifetime 与 Scope

- exchange-lifetime scope 由 Exchange 在读取请求正文前打开，跨越全部 Recovery RoundTrip 和下游响应；
- round-trip-lifetime scope 在每次 RoundTrip 构造上游请求前打开，Recovery retry、transport error 或响应 body 结束时关闭；
- exchange-lifetime Module 可以观察所有 round trip，round-trip-lifetime Module 不会泄漏状态到下一次重试；
- 两类 scope 的 `module.OpenContext` 与每次回调的 `module.Context` 自动携带同一份不可变 metadata；
- Program runtime 把两个 scope 中当前活跃的实例按原始 `modules` 数组索引合并；共同端口、mutation 和流投影都严格遵守同一个全局顺序；
- scope 结束时先 drain `scope_end` lane，再调用 Instance `Finish`；客户端取消只改变 Finish cause，不跳过 drain。

## 类型化端口

- Hook：提交前可变 Draft，例如 outbound request/body chunk、upstream response/body chunk；
- Transform：按 directive 顺序修改流数据；
- Stream：只读数据或派生投影，例如 raw chunk、JSON chunk、SSE data/comment；
- Fact：不可变生命周期事实，例如 request started、directive prepared、round trip started、Recovery transaction、body ended；
- Command：预留给异步影响未来状态的控制消息，不允许异步任务反向修改已经提交的数据。

Module 通过 `module.Registrar` 明确声明端口，端口值来自 `core/lifecycle`。未订阅的投影不会创建。例如 `builtin.llmusage` 只订阅 response headers、`UpstreamSSEData`、`UpstreamJSONChunk` 和 upstream end，不接收通用 raw Signal。

Recovery callback 是一等、只读的三阶段事务端口：`OnRecoveryStarted` 在调用 Controller 前投递，`OnRecoveryDecided` 在收到合法决策后投递，`OnRecoveryFinished` 在决策实际应用或失败后且在 `RoundTripFinished` 前投递。三个事件共享同一 `EventID`。`module.Context.Sequence` 是同一 Exchange Run 内单调递增的生命周期序号；Recovery 事件的 `module.Context.EventID` 与 payload 的 `EventID` 相同。Module 可以完整观察 trigger、Controller 请求上下文、决策和最终 outcome，但不能覆盖 directive 或 Controller 决策。

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

`core/program.Runtime` 负责 Program 编译、Run、Scope 和 Program Module 健康；Definition 的注册和全局名称唯一性属于 `core/module.Catalog`。Module 通过 `module.Context.Emitter` 产生可选外部 Record；Runtime 只依赖 `core/event.Provider`。

`core/event.Dispatcher` 实现该 provider，负责 `dp.event.v4` Record、单 trace sequence、buffer ownership、有界队列、分片和 Sink。Record 包含 `producer`、`topic`、`record_id`、`trace_id`、`metadata`、`round_trip`、`sequence`、`occurred_at` 和 `data`；Dispatcher 在顶层统一附加 metadata，因此 producer 不在 topic data 中重复它。Fluent 默认 tag 前缀为 `dp`。

`server.fluent.enabled=false` 时不创建 Dispatcher、Sink、Queue 或连接；Program runtime 使用 discard emitter，Module 仍注册、编译和执行，因此修改型 Module 不依赖事件输出。

Module panic 或回调错误会隔离该 Instance、使 barrier 失败，并将对应 Definition 的健康状态标为 degraded。`/health.modules` 与 `/health.event_output` 分别反映两个独立子系统。
