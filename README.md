# Directive Proxy

Directive Proxy 是由 `Authorization: Bearer dp.22.<inline|remote>.<base64url-json>.<hmac>` 指令驱动的通用 HTTP 反向代理。

项目的主要职责是 data plane：解析指令、改写请求、访问上游，并在异常发生时通过 Recovery Controller 让调用方同步修订远程指令或决定下一步动作。项目没有服务端控制面；directive 的生成、解析和校验全部在无登录的浏览器工作台本地完成。

## HTTP 边界

服务默认监听 `:23198`，单 listener 上的路由优先级如下：

- `GET /health`：公开健康检查；
- `GET /metrics`：公开 Prometheus 指标；
- 携带 `Authorization: Bearer dp.*` 的其他请求：进入 data plane；
- `GET/HEAD /` 及 `WEB_ROOT` 下实际存在的静态文件：提供浏览器工作台；
- 其他请求：返回 404。

除 `/health` 和 `/metrics` 外没有静态保留业务前缀；任意路径携带 dp token 都可以进入 data plane。

TokenSecret 位于 `server.proxy.directive.token-secret`，仅用于生成和校验 token HMAC；它不会写入 token。secret 错误或 MAC 篡改返回 `401 directive_unauthorized`。

上游 HTTPS 连接显式启用并优先协商 HTTP/2，服务端不支持时回退 HTTP/1.1；连接池由 `server.proxy.transport` 配置。明文 HTTP 保持 HTTP/1.1，不自动尝试 h2c。

前端只保留 directive workbench 和本地界面设置。`/console/exchanges`、活动 Exchange API、人工重试 API、OpenAPI/Docs 控制面均不存在。可观测事件由 Module 经 Fluent 输出到项目外部系统。

## Directive v22

当前 token 版本是 `22`，旧版本不兼容。Payload 使用服务端配置的 TokenSecret 计算 HMAC-SHA256：

```http
Authorization: Bearer dp.22.inline.<base64url-json>.<hmac>
Authorization: Bearer dp.22.remote.<base64url-json>.<hmac>
```

`hmac` 是 `HMAC-SHA256(TokenSecret, base64url-json)` 的 Base64URL 编码，其中 `base64url-json` 是 token 第四段的原始字符串。TokenSecret 只保存在服务端和生成 token 的工作台中，不写入 token。

inline token 的解码内容是：

```json
{
  "metadata": {"user_id": "user-1", "user_key": "key-1", "tenant_id": "tenant-a"},
  "target": {"base_url": "https://api.example.com/v1"},
  "headers": {
    "mutations": [
      {"side": "request", "action": "set", "name": "Authorization", "values": ["Bearer upstream-token"]},
      {"side": "response", "action": "del", "name": "Server"}
    ]
  },
  "recovery": {
    "controller": {"url": "https://control.example.com/recovery"},
    "triggers": {
      "unexpected_status": {
        "expected": [{"from": 200, "to": 299}]
      }
    },
    "budget": {"max_round_trips": 3}
  }
}
```

remote token 的解码内容是：

```json
{
  "http": {
    "url": "https://resolver.example.com/v1/team-a/service-a",
    "headers": {
      "mutations": [
        {"side": "request", "action": "set", "name": "Authorization", "values": ["Bearer resolver-token"]}
      ]
    }
  }
}
```

RemoteSpec 顶层必须且只能包含 `http`、`redis` 或 `file` 之一。Redis 使用标准连接 URL 与独立 key：

```json
{"redis":{"url":"redis://user:password@redis.example.com:6379/1","key":"team-a/service-a"}}
```

File 使用配置根目录内的 slash 相对路径：

```json
{"file":{"path":"team-a/services/primary.json"}}
```

文件根目录由 `server.proxy.directive.remote.file.root` 设置；支持子目录，但不接受绝对路径、`.`、`..` 或反斜杠路径。

Inline 的 JSON 本身就是 `Payload`。Remote 的 JSON 本身就是 `RemoteSpec`，只描述如何取得同一种 `Payload`；不能声明 `payload`、`modules`、`recovery` 或任何执行字段。

HTTP/Redis/File source 提供完整 `Payload`，例如：

```json
{
  "metadata": {"user_id": "user-1", "user_key": "key-1", "tenant_id": "tenant-a"},
  "target": {"base_url": "https://api.example.com/v2"},
  "modules": [
    {"module": "builtin.capture", "config": {}},
    {"module": "builtin.llmusage", "config": {"protocol": "openai.responses"}}
  ],
  "recovery": {
    "controller": {"url": "https://control.example.com/recovery"},
    "triggers": {"transport_error": true},
    "budget": {"max_round_trips": 3, "max_elapsed": "30s"}
  }
}
```

RemoteSpec 在请求 Prepare 阶段解引用一次。取得 Payload 后，inline 与 remote 进入完全相同的校验、编译和执行流程；不存在字段 merge、优先级、旧 plan 回退或每 RoundTrip 重读。Recovery retry 使用同一份已解析 Payload。

Payload 可以声明可选 metadata：最多 16 项、总计最多 8 KiB 的 `map<string,string>`，key 使用小写 snake_case。core 不要求任何业务身份字段，也不设置系统保留 key；`metadata` 包仅预设常用的 `user_id`、`user_key` key API。Exchange 生成的 UUIDv7 trace ID 是独立系统字段，不会在运行时注入 metadata；若 directive 自行声明 `trace_id`，它只是普通 metadata，与系统 trace 没有绑定关系。

Prepare 的唯一产物是不可变 `PreparedDirective`，固定包含 Source、HTTP Plan、Program、Recovery 和 Metadata。HTTP Plan 只拥有 HTTP 执行字段，不拥有 metadata。Exchange 在读取正文前一次性配置 directive facts、Program 和 Metadata，并打开 exchange-lifetime Module；RecoveryTransport 在第一个 RoundTrip 前从同一 PreparedDirective 安装已收紧的 Recovery budget。每次 RoundTrip 只打开新的 round-trip-lifetime Module，Module Context 自动携带同一份 metadata。

`target` 是严格 one-of，必须且只能包含 `base_url` 或 `exact_url`。`base_url` 作为反向代理基址，在 Prepare 阶段拼接入站 path 并追加入站 query；`exact_url` 是完整目标地址，忽略入站 path/query。编译后的最终 URL 写入不可变 Plan，Recovery round trip 复用同一个结果。

HTTP resolver 使用长生命周期连接池；HTTPS 显式启用并优先协商 HTTP/2，服务端不支持时回退 HTTP/1.1。resolver 与上游共用 `server.proxy.transport` 配置，但使用相互隔离的 transport 实例；明文 HTTP 保持 HTTP/1.1，不自动尝试 h2c。

Payload 的 `headers` 是单一 HeaderPolicy。每条 mutation 都必须声明 `side: request|response`，action 与 Go `http.Header` 对齐，只允许 `add|set|del`：`add` 追加一个或多个值，`set` 必须且只能设置一个值，`del` 删除名称对应的全部值且不能包含 `values`。数组顺序就是应用顺序；需要从空集合重建 Header 时，先声明 `{"side":"request","action":"del","glob":"*"}`。`preserve_proxy_disclosure` 只作用于 request。HTTP RemoteSpec 复用同一结构，但只允许 request side。请求始终以原 Header 为基线，directive Authorization、原 Content-Length 和默认不保留的代理披露 Header 在 mutations 前移除；最后统一移除 `x-dp-*` 与 hop-by-hop Header。最终未设置 User-Agent 时会抑制 Go transport 的默认 User-Agent。

## Recovery Controller

Recovery 是唯一的自动恢复机制。没有独立 Retry API、Retry-ID 或活动请求索引。

`controller` 直接声明控制面 HTTP callback 的 `url`、可选 `headers` 和可选 `timeout`；默认 timeout 为 `3s`。Prepare 阶段由 Recovery HTTP adapter 将这些参数编译为不可变 Controller Binding，并由同一 Recovery Policy 跨全部 RoundTrip 复用。Controller 不是 Program Module，不注册到 Module Catalog；RecoveryTransport 也不持有全局 Controller。

可配置触发器：

- `unexpected_status`：上游状态码不在 `expected` 范围内；
- `response_header_timeout`：请求正文写完后，在指定时间内没有收到最终响应头；
- `transport_error`：连接、TLS、读写等传输失败；

完整策略示例：

```json
{
  "controller": {
    "url": "https://control.example.com/recovery",
    "headers": {"Authorization": "Bearer recovery-secret"},
    "timeout": "3s"
  },
  "triggers": {
    "response_header_timeout": "10s",
    "unexpected_status": {
      "expected": [
        {"from": 200, "to": 299},
        {"from": 304, "to": 304}
      ],
      "capture_body_bytes": 65536
    },
    "transport_error": true
  },
  "budget": {
    "max_round_trips": 3,
    "max_elapsed": "30s"
  }
}
```

Controller 接收同步 `POST`，协议为 `dp.recovery.v3`。`Idempotency-Key` 等于确定性的 `event_id`：

```json
{
  "protocol": "dp.recovery.v3",
  "event_id": "0198...:1:unexpected_status",
  "trace_id": "0198...",
  "observed_at": "2026-07-17T08:00:00Z",
  "round_trip": {
    "number": 1,
    "max_round_trips": 3,
    "elapsed_ms": 142,
    "remaining_ms": 29858,
    "next_round_trip": 2,
    "retry_allowed": true
  },
  "trigger": {"type": "unexpected_status"},
  "directive": {
    "mode": "remote",
    "backend": "http",
    "endpoint": "https://resolver.example.com/v1/team-a/service-a",
    "payload_sha256": "..."
  },
  "metadata": {
    "user_id": "user-1",
    "user_key": "key-1",
    "tenant_id": "tenant-a"
  },
  "response": {
    "status_code": 401,
    "headers": {"Content-Type": ["application/json"]},
    "body": {
      "encoding": "base64",
      "data": "eyJlcnJvciI6ImV4cGlyZWQifQ==",
      "size": 19,
      "truncated": false
    }
  }
}
```

`response` 只在 `unexpected_status` 时存在。响应头完整回传；正文按 directive 与服务端上限截断并使用 base64，`size` 优先表示已知的原始 Content-Length。观测信息保留完整 endpoint 和 query；Redis key 或 File path 记录为 `resource`。认证信息和底层 adapter 错误不脱敏。

Controller 必须返回一个小型 JSON 决策：

```json
{"action":"retry","after_ms":100}
```

- `retry`：可选延迟后取消当前 RoundTrip，并使用同一份已解析 Payload 重放原请求正文；
- `forward`：对异常状态响应继续转发原始状态、响应头和完整正文；对没有响应可转发的错误则保留原失败路径；
- `fail`：终止当前请求并映射为 recovery failure。

Recovery 只发生在下游响应提交之前。SSE 或其他流式响应一旦响应头已提交，就不会被透明拼接、重连或替换。POST/PATCH 只有在初始请求带有 `Idempotency-Key` 时才允许 `retry`；上游仍必须实现正确的幂等语义。

Controller 回调失败、超时或返回非法决策时，代理保留原始结果：异常状态继续转发，传输错误继续按原错误失败。全局资源上限位于 `server.proxy.recovery`，会收紧 directive 声明的 RoundTrip、总时长、回调超时和正文捕获大小。

## Replay Store 与 Exchange

每个入站请求在进程内建模为一个 `Exchange`，并通过 `X-Dp-Trace-ID` 返回 UUIDv7 trace ID。Exchange 只拥有生命周期和当前 RoundTrip，不提供查询 API 或跨请求身份。

请求正文使用单写入、多 reader 的流式 Replay Store。当前 RoundTrip 可以在客户端仍上传时读取正文；Recovery 决定 `retry` 后，新 RoundTrip 从 offset 0 重放已有前缀，追上尾部后继续等待后续数据。正文始终保存在进程内分段内存中；读取前先申请有界 reservation，内存不足时请求进入 FIFO，队列满或超时返回 `503`，便于多实例调用方重新分流。Directive 可覆盖本请求的正文上限、排队等待、读取超时和 chunk 大小。

详细状态机见 [Exchange lifecycle](docs/exchange-lifecycle.md)。

## Module 与外部观测

内置 Module 由 directive 的单一有序 `modules` 数组启用；每项只声明唯一的 `module` 和可选 `config`，生命周期由 Module Definition 静态声明：

- [`builtin.capture`](docs/module-capture.md)：请求、响应和生命周期审计；
- [`builtin.llmusage`](docs/module-llmusage.md)：LLM token usage 提取；
- [`builtin.llmperf`](docs/module-llmperf.md)：LLM 响应性能测量。

Module 经内部有界队列向 Fluent 输出统一 `dp.event.v6` Record，默认 Fluent tag 前缀为 `dp`。每条 Record 使用 `(trace_id, sequence)` 作为事件身份，并在顶层携带完整 directive `metadata`；各 topic 的 `data` 不重复这些公共字段。Capture、LLM usage 等所有 producer 使用相同语义。`server.fluent.enabled=false` 时不创建 Sink、Queue 或连接，但 Module 仍注册、校验和执行。观测查询和展示应部署在 Fluent 下游，不放回本项目控制面。

服务在公开的 `GET /metrics` 暴露 Prometheus 兼容指标。应用指标名的完整前缀通过 `server.metrics.prefix` 配置，默认是 `m_260628_`；标准 `go_*` 和 `process_*` 指标不受影响。

默认同时输出 Go runtime 和 OS process 指标；抓取方可使用 `?runtime=0`、`?process=0` 分别关闭它们，例如 `/metrics?runtime=0&process=0` 只返回应用指标。Prometheus 可通过 scrape job 的 `params` 配置这些开关。

```yaml
server:
  metrics:
    prefix: m_260628_
```

已解析的 `Payload.modules` 在 Prepare 阶段编译一次为不可变 Program Executable；exchange-lifetime 实例打开一次，Recovery 的每个 RoundTrip 仅从同一批 Binding 打开新的 round-trip-lifetime 实例，不重新编译 Module 配置。数组顺序是所有当前活跃 Module 的全局执行顺序；Module 名在 Program Module Catalog 内唯一，并直接作为外部 Record 的 `producer`。Recovery Controller 使用独立的类型化 HTTP 参数和 Binding，不属于 Module Catalog。

更多细节见 [Module architecture](docs/module-architecture.md)。

## 指令工作台

工作台是不带登录、Session 或应用 Cookie 状态的静态前端。directive JSON 与 token 的编解码完全在浏览器本地完成，不调用服务端 encode API；TokenSecret 只保存在当前页面内存中。请求调试器显式使用 `credentials: "omit"`，不会携带或接受浏览器凭据。

主题和语言偏好保存在浏览器 localStorage 中，不参与访问控制、directive 或代理请求。

前端开发：

```shell
pnpm install
pnpm dev
```

生产镜像把 `pnpm build` 生成的 `dist/` 复制到 `/app/web`，并通过 `WEB_ROOT=/app/web` 提供工作台入口和静态资源。前端使用 HashRouter，不依赖服务端 SPA fallback。

## Directive 来源白名单

`server.proxy.directive.source-access` 只保护 dp data plane，不保护健康检查、指标或静态前端。启用后，来源校验发生在 token 解码和远程 resolver 访问之前。

规则支持自动识别的 IP、CIDR 和域名。只有直接对端命中 `trusted-proxies` 时才按 `headers` 的配置顺序读取转发头；`max-header-bytes` 和 `max-hops` 限制转发链的资源消耗。

## 构建与验证

完整配置见 [config.example.yaml](config/config.example.yaml)。

```shell
go test ./...
go build ./...
pnpm build
```

相关设计文档：

- [Remote directive adapter](docs/directive-remote-adapter-design.md)
- [Exchange lifecycle](docs/exchange-lifecycle.md)
- [Module architecture](docs/module-architecture.md)
