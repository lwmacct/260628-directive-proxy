# Directive Proxy

Directive Proxy 是由 `Authorization: Bearer dp.<version>.<inline|remote>.<base64url-json>` 指令驱动的通用 HTTP 反向代理。

项目的主要职责是 data plane：解析指令、改写请求、访问上游，并在异常发生时通过 Recovery Controller 让调用方同步修订远程指令或决定下一步动作。服务端控制面只保留 AuthMe 登录；directive 的生成、解析和校验全部在浏览器工作台本地完成。

## HTTP 边界

服务默认监听 `:23198`，单 listener 上的路由优先级如下：

- `/authme/*`：AuthMe 登录、回调和 Session API；
- `GET /health`：公开健康检查；
- `/api/admin/*`、`/api/public/*`：保留前缀，统一返回 404；
- 携带 `Authorization: Bearer dp.*` 的其他请求：进入 data plane；
- 其他 GET/HEAD 请求：在设置 `WEB_ROOT` 时由 SPA 文件服务器处理，否则返回 404。

`/api/admin/*` 和已删除的 `/api/public/*` 都是保留前缀，不会因为携带 dp token 而进入代理。普通业务路径（包括其他 `/api/...`）仍可进入 data plane。

上游 HTTPS 连接显式启用并优先协商 HTTP/2，服务端不支持时回退 HTTP/1.1；连接池由 `server.proxy.transport` 配置。明文 HTTP 保持 HTTP/1.1，不自动尝试 h2c。

前端只保留 directive workbench、登录和本地界面设置。`/console/exchanges`、活动 Exchange API、人工重试 API、OpenAPI/Docs 控制面均不存在。可观测事件由 Module 经 Fluent 输出到项目外部系统。

## Directive v18

当前 token 版本是 `18`，旧版本不兼容：

```http
Authorization: Bearer dp.18.inline.<base64url-json>
Authorization: Bearer dp.18.remote.<base64url-json>
```

inline token 的解码内容是：

```json
{
  "target": {"url": "https://api.example.com/v1"},
  "headers": {
    "mode": "patch",
    "mutations": [
      {"side": "request", "action": "set", "name": "Authorization", "values": ["Bearer upstream-token"]},
      {"side": "response", "action": "remove", "name": "Server"}
    ]
  },
  "recovery": {
    "controller": {"url": "https://control.example.com/recovery"},
    "triggers": {
      "unexpected_status": {
        "expected": [{"from": 200, "to": 299}]
      }
    },
    "budget": {"max_attempts": 3}
  }
}
```

remote token 的解码内容是：

```json
{
  "http": {
    "url": "https://resolver.example.com/v1/team-a/service-a",
    "headers": {
      "mode": "patch",
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

Inline 的 JSON 本身就是 `Payload`。Remote 的 JSON 本身就是 `RemoteSpec`，只描述如何取得同一种 `Payload`；不能声明 `payload`、`program`、`recovery` 或任何执行字段。

HTTP/Redis/File source 提供完整 `Payload`，例如：

```json
{
  "target": {"url": "https://api.example.com/v2"},
  "program": {
    "request": [
      {"id": "capture", "module": "builtin.capture", "config": {}}
    ],
    "attempt": [
      {"id": "usage", "module": "builtin.llmusage", "config": {"protocol": "openai.responses"}}
    ]
  },
  "recovery": {
    "controller": {"url": "https://control.example.com/recovery"},
    "triggers": {"transport_error": true},
    "budget": {"max_attempts": 3, "max_elapsed": "30s"}
  }
}
```

RemoteSpec 在请求 Prepare 阶段解引用一次。取得 Payload 后，inline 与 remote 进入完全相同的校验、编译和执行流程；不存在字段 merge、优先级、旧 plan 回退或每 Attempt 重读。Recovery retry 使用同一份已解析 Payload。

HTTP resolver 使用长生命周期连接池；HTTPS 显式启用并优先协商 HTTP/2，服务端不支持时回退 HTTP/1.1。resolver 与上游共用 `server.proxy.transport` 配置，但使用相互隔离的 transport 实例；明文 HTTP 保持 HTTP/1.1，不自动尝试 h2c。

Payload 的 `headers` 是单一 HeaderPolicy。每条 mutation 都必须声明 `side: request|response`，action 只允许 `set|remove|append`；数组顺序就是应用顺序。`mode` 和 `preserve_proxy_disclosure` 只作用于 request。HTTP RemoteSpec 直接复用同一结构，但只允许 request side，因为它描述的是 resolver 请求本身。默认 patch 以原请求头为基线：directive Authorization 与原 Content-Length 在 mutations 前移除，代理披露头默认移除；mutations 可以重新设置 resolver Authorization；最后统一移除 `x-dproxy-*` 和 hop-by-hop headers。

## Recovery Controller

Recovery 是唯一的自动恢复机制。没有独立 Retry API、Retry-ID 或活动请求索引。

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
    "max_attempts": 3,
    "max_elapsed": "30s"
  }
}
```

Controller 接收同步 `POST`，协议为 `dproxy.recovery.v1`。`Idempotency-Key` 等于确定性的 `event_id`：

```json
{
  "protocol": "dproxy.recovery.v1",
  "event_id": "0198...:1:unexpected_status",
  "trace_id": "0198...",
  "observed_at": "2026-07-17T08:00:00Z",
  "attempt": {
    "number": 1,
    "max_attempts": 3,
    "elapsed_ms": 142,
    "remaining_ms": 29858,
    "next_attempt": 2,
    "retry_allowed": true
  },
  "trigger": {"type": "unexpected_status"},
  "directive": {
    "mode": "remote",
    "backend": "http",
    "endpoint": "https://resolver.example.com/v1/team-a/service-a",
    "payload_sha256": "..."
  },
  "metadata": {"X-Dproxy-Request-Id": ["request-1"]},
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

- `retry`：可选延迟后取消当前 Attempt，并使用同一份已解析 Payload 重放原请求正文；
- `forward`：对异常状态响应继续转发原始状态、响应头和完整正文；对没有响应可转发的错误则保留原失败路径；
- `fail`：终止当前请求并映射为 recovery failure。

Recovery 只发生在下游响应提交之前。SSE 或其他流式响应一旦响应头已提交，就不会被透明拼接、重连或替换。POST/PATCH 只有在初始请求带有 `Idempotency-Key` 时才允许 `retry`；上游仍必须实现正确的幂等语义。

Controller 回调失败、超时或返回非法决策时，代理保留原始结果：异常状态继续转发，传输错误继续按原错误失败。全局资源上限位于 `server.proxy.recovery`，会收紧 directive 声明的 Attempt、总时长、回调超时和正文捕获大小。

## Replay Store 与 Exchange

每个入站请求在进程内建模为一个 `Exchange`，并通过 `X-Dproxy-Trace-ID` 返回 UUIDv7 trace ID。Exchange 只拥有生命周期和当前 Attempt，不提供查询 API 或跨请求身份。

请求正文使用单写入、多 reader 的流式 Replay Store。当前 Attempt 可以在客户端仍上传时读取正文；Recovery 决定 `retry` 后，新 Attempt 从 offset 0 重放已有前缀，追上尾部后继续等待后续数据。小正文保存在分段内存中，超过阈值或全局内存紧张时转入匿名临时文件。

详细状态机见 [Exchange lifecycle](docs/exchange-lifecycle.md)。

## Module 与外部观测

内置 Module 由 directive 的有序 `program.request` / `program.attempt` 启用：

- [`builtin.capture`](docs/module-capture.md)：请求、响应和生命周期审计；
- [`builtin.llmusage`](docs/module-llmusage.md)：LLM token usage 提取；
- [`builtin.llmperf`](docs/module-llmperf.md)：LLM 响应性能测量。

Module 经内部有界队列输出统一 `dproxy.event.v2` Record。`server.fluent.enabled=false` 时不创建 Sink、Queue 或连接，但 Module 仍注册、校验和执行。观测查询和展示应部署在 Fluent 下游，不放回本项目控制面。

更多细节见 [Module architecture](docs/module-architecture.md)。

## AuthMe 与指令工作台

AuthMe 支持静态 Access token 和 Dex GitHub OIDC。至少启用一种认证方式。浏览器使用统一加密 Session；directive 的 JSON 与 token 编解码完全在工作台本地完成，不调用服务端 encode API。

```yaml
server:
  http:
    authme:
      origins:
        - https://proxy.example.com
      session:
        keys:
          - id: primary
            secret: "${AUTHME_SESSION_KEY}"
      statictoken:
        enabled: true
        credentials:
          - id: admin
            name: Administrator
            token: "${AUTHME_ACCESS_TOKEN}"
      dexgithub:
        enabled: false
```

`AUTHME_SESSION_KEY` 必须是 base64url 编码的 32 字节值。生产 OIDC callback 为 `<origin>/authme/callback/github`。

前端开发：

```shell
pnpm install
pnpm dev
```

生产镜像把 `pnpm build` 生成的 `dist/` 复制到 `/app/web`，并通过 `WEB_ROOT=/app/web` 提供 SPA。

## Directive 来源白名单

`server.proxy.directive.source-access` 只保护 dp data plane，不保护 AuthMe、健康检查或静态前端。启用后，来源校验发生在 token 解码和远程 resolver 访问之前。

规则支持 IP、CIDR 和域名。只有直接对端命中 `trusted-proxies` 时才读取 `Forwarded`、`X-Forwarded-For` 或 `X-Real-IP`。

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
