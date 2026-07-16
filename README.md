# Directive Proxy

Directive Proxy 是由 `Authorization: Bearer dproxy.<version>...` 指令驱动的通用 HTTP 反向代理。

项目的主要职责是 data plane：解析指令、改写请求、访问上游，并在异常发生时通过 Recovery Controller 让调用方同步修订远程指令或决定下一步动作。项目只保留一个最小控制面，用于登录和生成/校验 directive；活动请求观测、外部重试接口和 Retry-ID 已全部移除。

## HTTP 边界

服务默认监听 `:23198`，单 listener 上的路由优先级如下：

- `POST /api/admin/directives/encode`、`decode`、`validate`：受 AuthMe 保护的指令工具 API；
- `/authme/*`：AuthMe 登录、回调和 Session API；
- `GET /health`：公开健康检查；
- 携带 `Authorization: Bearer dproxy.*` 的其他请求：进入 data plane；
- 其他 GET/HEAD 请求：在设置 `WEB_ROOT` 时由 SPA 文件服务器处理，否则返回 404。

`/api/admin/*` 和已删除的 `/api/public/*` 都是保留前缀，不会因为携带 dproxy token 而进入代理。普通业务路径（包括其他 `/api/...`）仍可进入 data plane。

前端只保留 directive workbench、登录和本地界面设置。`/console/exchanges`、活动 Exchange API、人工重试 API、OpenAPI/Docs 控制面均不存在。可观测事件由 Module 经 Fluent 输出到项目外部系统。

## Directive v18

当前 token 版本是 `18`，旧版本不兼容：

```http
Authorization: Bearer dproxy.18.i.<base64url-json>
Authorization: Bearer dproxy.18.r.<base64url-json>
```

inline token 的解码内容是：

```json
{
  "payload": {
    "target": {"url": "https://api.example.com/v1"},
    "headers": {
      "request": {
        "ops": [
          {"op": "=", "name": "Authorization", "values": ["Bearer upstream-token"]}
        ]
      }
    }
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
  "source": {
    "type": "http",
    "url": "https://resolver.example.com/v1/directive",
    "key": "team-a/service-a",
    "headers": {"Authorization": "Bearer resolver-token"},
    "request_headers": ["Content-Type", "X-Tenant"]
  },
  "program": {
    "request": [
      {"id": "capture", "module": "builtin.capture", "config": {}}
    ]
  },
  "recovery": {
    "controller": {"url": "https://control.example.com/recovery"},
    "triggers": {"transport_error": true},
    "budget": {"max_attempts": 3, "max_elapsed": "30s"}
  }
}
```

稳定 token 层拥有 `source`、request-scope `program` 和 `recovery`。HTTP/Redis resolver 返回的仍是裸 `Payload`，例如：

```json
{
  "target": {"url": "https://api.example.com/v2"},
  "program": {
    "attempt": [
      {"id": "usage", "module": "builtin.llmusage", "config": {"protocol": "openai.responses"}}
    ]
  }
}
```

remote Payload 不得声明 request-scope program，也不能覆盖稳定 Recovery 策略。每个 Attempt 都重新读取和编译 remote Payload；不缓存 value、不回退旧 plan、不递归解析另一条 remote token。这样 Controller 在返回 `retry` 前完成远程指令修改，下一次 Attempt 就会读取新版本。

## Recovery Controller

Recovery 是唯一的自动恢复机制。没有独立 Retry API、Retry-ID 或活动请求索引。

可配置触发器：

- `unexpected_status`：上游状态码不在 `expected` 范围内；
- `response_header_timeout`：请求正文写完后，在指定时间内没有收到最终响应头；
- `transport_error`：连接、TLS、读写等传输失败；
- `directive_error`：当前 Attempt 的远程指令读取或编译失败。

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
    "transport_error": true,
    "directive_error": true
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
    "endpoint": "https://resolver.example.com/v1/directive",
    "key": "team-a/service-a",
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

`response` 只在 `unexpected_status` 时存在。响应头完整回传；正文按 directive 与服务端上限截断并使用 base64，`size` 优先表示已知的原始 Content-Length。URL 中的 userinfo、query 和 fragment 不会写入事件中的 resolver endpoint。

Controller 必须返回一个小型 JSON 决策：

```json
{"action":"retry","after_ms":100}
```

- `retry`：可选延迟后取消当前 Attempt，重放原请求正文，并重新读取 remote Payload；
- `forward`：对异常状态响应继续转发原始状态、响应头和完整正文；对没有响应可转发的错误则保留原失败路径；
- `fail`：终止当前请求并映射为 recovery failure。

Recovery 只发生在下游响应提交之前。SSE 或其他流式响应一旦响应头已提交，就不会被透明拼接、重连或替换。POST/PATCH 只有在初始请求带有 `Idempotency-Key` 时才允许 `retry`；上游仍必须实现正确的幂等语义。

Controller 回调失败、超时或返回非法决策时，代理保留原始结果：异常状态继续转发，传输/指令错误继续按原错误失败。全局资源上限位于 `server.proxy.recovery`，会收紧 directive 声明的 Attempt、总时长、回调超时和正文捕获大小。

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

AuthMe 支持静态 Access token 和 Dex GitHub OIDC。至少启用一种认证方式。浏览器使用统一加密 Session，自动化客户端也可以直接携带 AuthMe Access token 调用 `/api/admin/directives/*`。

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

`server.proxy.directive.source-access` 只保护 dproxy data plane，不保护 AuthMe、指令工具 API、健康检查或静态前端。启用后，来源校验发生在 token 解码和远程 resolver 访问之前。

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
