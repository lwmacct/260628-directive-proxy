# 260628-directive-proxy

`260628-directive-proxy`（Directive Proxy）是由 directive token 驱动的通用 HTTP 反向代理 data plane。

`dproxy` 是 Directive Proxy 的协议前缀，当前用于 `dproxy.15.*` directive token 和 `dproxy.resolve.v1` 远端解析协议。

项目只负责解析 `Authorization: Bearer dproxy.15...` 中的 directive，按 directive 改写请求并转发到目标上游。

服务仅使用一个 HTTP listener，默认监听 `:23198`：

- 携带 `Authorization: Bearer dproxy.*` 且通过来源白名单的请求进入基于原生 `net/http` 的反向代理。
- `/api/public/*` 进入匿名请求协作 API，`/api/control/*` 进入受认证保护的管理 API。
- 其他请求进入 `/health`、认证端点或可选 Web UI。

`/api/public/*` 和 `/api/control/*` 是系统保留前缀，优先于 dproxy token 分流；其他路径（包括普通 `/api/...`）携带 dproxy token 时仍可进入代理。代理流量不经过 Huma，避免流式响应、请求体和上游 header 被 API 框架额外处理。

## 等待响应请求与外部介入重试

代理为每个进入 data plane 的逻辑请求生成 canonical UUIDv7 `trace_id`，并通过响应头 `X-Dproxy-Trace-ID` 返回。一次逻辑请求可以包含多个上游 attempt；外部 API 介入会取消当前尚未收到最终响应头的 attempt，并从同一份不可变内存正文启动下一次 attempt。Remote directive 在每个 attempt 都重新读取和编译，不合并或回退旧 plan。

Proxy Retry 是每个代理请求的固有能力；入站请求无需携带 retry identity 即可正常转发。需要通过 Public API 介入自己的请求时，调用方携带一个 canonical UUIDv7 `Dproxy-Retry-ID`。该 header 是 bearer retry credential，代理在任何日志、插件或上游处理前移除它，只保存 SHA-256 verifier：

- `PUT /api/public/retry`：无需 Control Auth，但必须携带 `Dproxy-Retry-ID: <uuidv7>` 和 `If-Match: "attempt:<current_attempt>"`；服务端自动计算下一 attempt。
- `GET /api/control/proxy-requests`：认证后列出活动请求。
- `GET /api/control/proxy-requests/{trace_id}`：认证后读取一个活动请求。
- `PUT /api/control/proxy-requests/{trace_id}/retry`：认证后按 trace ID 介入，并携带 `If-Match: "attempt:<current_attempt>"`。

重复提交同一个已接受的 PUT 会返回原结果，不会再次取消 attempt；终态结果按 `command-retention` 短期保留。收到最终响应头后，请求立即退出可重试集合。`text/event-stream` 响应因此只在建立 SSE 之前可重试；已经开始传输的 SSE 不会被透明拼接或重连。POST/PATCH 只有在初始请求携带 `Idempotency-Key` 时才允许重试；代理会在所有 attempt 强制保留原值，但上游仍需正确实现幂等语义。

带正文的请求必须提供 `Content-Length`。代理先按字节预算进入严格 FIFO 等待队列，获得额度前不会调用 `Body.Read`；获得额度后一次性分配准确长度的连续 `[]byte`。正文由逻辑请求、重试 attempt 和异步 Capture 通过 lease 共享，最后一个引用结束时归还额度，不写本地磁盘。响应继续流式转发；同步插件借用当前响应 buffer，异步 Capture 只复制固定大小 chunk，并受独立内存预算限制。

活动控制器和 cancel 句柄属于当前进程；多实例部署必须让 Control API 命中持有原请求连接的实例，例如使用实例级管理地址或粘性路由。

```yaml
proxy:
  retry:
    enabled: true
    max-attempts: 3
    command-retention: 1m
  body-memory:
    max-active-bytes: 2147483648
    max-body-bytes: 33554432
    queue-max-requests: 512
    queue-max-wait: 15s
    body-read-timeout: 30s
observability:
  response-capture-memory:
    max-retained-bytes: 268435456
    overflow: drop
```

## 可观测插件与输出

代理只产生进程内生命周期 Signal；内置观测插件把 Signal 转换为统一的 `dproxy.event.v1` Record，输出插件负责外部投递。当前提供：

- `builtin.capture`：请求/响应 header、Metadata、body chunk/hash、attempt、SSE 语义事件和完成状态。
- `builtin.llmusage`：从上游 JSON/SSE 响应增量提取 OpenAI Responses、OpenAI Chat Completions、Anthropic Messages 和 Google GenerateContent token usage。
- `builtin.llmperf`：从上游响应时间线计算 TTFB、TTFT、TTFC、生成完成和端到端延迟等性能指标。
- `fluent` output：按 topic 路由 Record，经有界队列异步发送到 Fluent Forward endpoint。

Capture 解析成功写给下游的响应字节；LLM Usage 解析代理从上游实际读取的响应字节。请求 Capture 通过 lease 引用 canonical body，响应插件同步借用流式 buffer；需要异步保留的响应 Capture chunk 只复制一次。Record 再按 `trace_id` 分片进入异步输出队列，并在最后一个 output 完成后自然释放资源。输出故障和解析失败均 fail-open，不改变代理响应；队列溢出与输出故障会在 `/health` 的 `observability` 字段报告 degraded。

插件和输出必须成对启用；完整配置见 [`config/config.example.yaml`](config/config.example.yaml)，架构和扩展约束见 [`docs/observability-plugins.md`](docs/observability-plugins.md)。

完整事件契约与部署约束见 [Proxy request lifecycle](docs/proxy-request-lifecycle.md)。

Control API 支持 Dex OIDC 和静态 Access token 两种认证模式。`/api/control/*` 必须通过当前模式认证；`/api/public/proxy-requests/*`、`/health` 和 Web UI 不要求 Control Auth。dproxy 代理流量在解析 token 或访问远端 resolver 前先执行来源校验。

## Control API 登录

`server.http.auth.methods` 是唯一的认证启用状态，可包含 `token`、`oidc` 或两者，默认只启用 `token`。列表顺序决定登录方式顺序；只有启用的配置会在启动时校验和初始化。

同时启用时，浏览器登录页以 Access token 表单为主体，并提供可选的 GitHub 登录按钮。两种方式签发同一个加密浏览器 Session；显式 Bearer 无效时不会降级使用 Session Cookie。

所有模式共用可信 origin 和 Session key ring：

```yaml
server:
  http:
    auth:
      external-urls:
        - https://proxy.example.com
      session:
        keys:
          - id: primary
            secret: "${AUTH_SESSION_KEY}"
        ttl: 24h
```

`AUTH_SESSION_KEY` 必须是 base64url 编码的 32 字节随机值，可用 `openssl rand -base64 32 | tr '+/' '-_' | tr -d '='` 生成。第一把 key 用于写入，所有 key 均可解密，便于轮换。

### Access token 模式

使用 OpenSSL 生成 24 个随机字节的无填充 Base64URL secret，并组装 token：

```shell
_id=admin
_secret="$(openssl rand -base64 24 | tr '+/' '-_' | tr -d '=')"
AUTH_TOKEN="dpctl.10.${_id}.${_secret}"
```

将完整 token 计算为服务端配置需要的 SHA-256 摘要：

```shell
AUTH_TOKEN_SHA256="$(printf '%s' "${AUTH_TOKEN}" | openssl sha256 -r | awk '{print $1}')"
printf 'AUTH_TOKEN=%s\nAUTH_TOKEN_SHA256=%s\n' "${AUTH_TOKEN}" "${AUTH_TOKEN_SHA256}"
```

token 固定使用 `dpctl.10.<credential-id>.<secret>` 格式，其中 secret 是 24 个随机字节的无填充 Base64URL 编码。旧式任意字符串、UUID、其他版本、非规范 Base64URL 和带前后空白的 token 均不接受。

通过环境变量注入摘要，避免把原始 token 或摘要提交到仓库：

```shell
export AUTH_TOKEN_SHA256="<token-sha256>"
export AUTH_SESSION_KEY="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=')"
```

默认配置会读取该摘要环境变量；未设置、为空或不是 64 字符小写十六进制时，服务会拒绝启动。显式配置方式如下：

```yaml
server:
  http:
    auth:
      methods:
        - token
      token:
        credentials:
          admin:
            name: Administrator
            token-sha256: "${AUTH_TOKEN_SHA256}"
```

浏览器输入 token 后，服务只把 credential ID 和完整 secret revision 写入统一加密 Session，不保存原始 token。删除 credential 或轮换摘要会立即撤销对应登录，不需要 Session 数据库。

自动化客户端无需调用登录端点，可直接访问 Control API：

```http
Authorization: Bearer <access-token>
```

配置使用 credential ID 到摘要的映射，支持多个凭据以便审计和轮换。ID 最长 56 字节，只接受小写字母、数字、`-` 和 `_`，并且必须以字母或数字开头、结尾。

### OIDC 模式

启用 OIDC 时连接 Dex，使用 public client、Authorization Code Flow 和 S256 PKCE：

```yaml
server:
  http:
    auth:
      methods:
        - oidc
      external-urls:
        - http://localhost:23199
      oidc:
        issuer: https://2008.s.lwmacct.com:20088
        client-id: dproxy
        allowed-users:
          - lwmacct
        session-ttl: 24h
```

`allowed-users` 对 Dex `preferred_username` 中的 GitHub 用户名执行忽略大小写的精确匹配。服务仍验证 `federated_claims.connector_id == github` 并保留 GitHub 数字用户 ID，用于身份响应、头像和审计日志；数字 ID 不参与本地授权配置。

登录回调验证 issuer、audience、签名、有效期、nonce、PKCE 和 GitHub connector 后签发本地 AES-256-GCM Session；Cookie 不保存 Dex ID Token 或 GitHub access token。本地管理员策略在每次请求时重新执行。

生产部署必须为每个工具注册独立 Dex client，并配置 HTTPS `external-urls`。OIDC callback 固定为 `<external-url>/auth/callback/github`，且必须全部注册到 Dex client。服务按请求 Host 精确选择 origin；不同域名各自持有 Host-only Cookie。

当前不支持且暂不计划直接连接 GitHub 原生 OAuth App。现有认证边界保持为标准 OIDC：provider 必须提供 OIDC discovery 和可验证的 ID Token，GitHub 身份由 Dex GitHub connector 转换为 OIDC claims。这样无需在服务内持有临时 GitHub access token，也避免引入 GitHub API、限流和单 callback URL 等额外语义。

同时提供两种登录方式时配置：

```yaml
server:
  http:
    auth:
      methods:
        - token
        - oidc
      external-urls:
        - https://proxy.example.com
      token:
        credentials:
          admin:
            name: Administrator
            token-sha256: "${AUTH_TOKEN_SHA256}"
      oidc:
        issuer: https://auth.example.com
        client-id: dproxy
        allowed-users:
          - octocat
        session-ttl: 24h
```

## Directive 来源白名单

`proxy.directive.source-access` 只保护携带 `Authorization: Bearer dproxy.*` 的 Directive 流量。Control API、OIDC、`/health` 和 Web UI 继续使用各自的访问策略。来源白名单默认禁用；启用后仅允许 `allowed-sources` 中配置的来源。

```yaml
proxy:
  directive:
    source-access:
      enabled: true
      allowed-sources:
        - 127.0.0.1
        - ::1
        - 172.22.0.0/16
        - client.example.net
      trusted-proxies:
        - 172.18.0.0/16
      dns:
        lookup-timeout: 2s
        success-ttl: 1m
        failure-ttl: 10s
        stale-ttl: 10m
        max-hosts: 1024
```

`allowed-sources` 接受精确 IP、CIDR 或域名，并按 OR 关系匹配。域名通过正向 A/AAAA 解析后与客户端 IP 比较，不检查请求 Host，也不执行 PTR 反查。解析结果按请求惰性缓存；刷新失败时只在 `stale-ttl` 窗口内继续使用旧的成功结果。

默认以 TCP 对端地址作为客户端 IP。仅当直接对端命中 `trusted-proxies` 时，才按 `Forwarded`、`X-Forwarded-For`、`X-Real-IP` 的优先级读取转发链，并从右向左剥离可信代理。`trusted-proxies` 只接受 IP/CIDR；可信反向代理必须覆盖或清理客户端传入的转发头。优先级更高的转发头格式非法时会直接以 `source_invalid` 拒绝，不回退到其他头。

启用来源白名单时，空白名单、非法或重复规则、非法 DNS 参数都会使服务启动失败。来源未命中返回 `403 source_not_allowed`，无法确定有效客户端地址返回 `403 source_invalid`。

## Directive Token

唯一入口是：

```http
Authorization: Bearer dproxy.15.i.<base64url-directive-json>
Authorization: Bearer dproxy.15.r.<base64url-remote-spec-json>
```

`dproxy.` token family 由代理保留，当前只接受 `dproxy.15.i` 和 `dproxy.15.r` 四段协议。其他 Bearer token 不会进入代理，旧版 token 不再兼容。

- `i` 直接从 token 读取完整 directive JSON。
- `r` 从 token 读取自包含的 HTTP 或 Redis `RemoteSpec`，远端响应就是完整 directive JSON。
- 远端 directive 不与 token 合并、不回退、不缓存 value，也不能递归引用另一条 remote directive。

payload schema：

```json
{
  "target": {
    "url": "https://api.example.com/v1"
  },
  "proxy": "socks5://user:pass@127.0.0.1:1080",
  "headers": {
    "mode": "patch",
    "ops": [
      {
        "op": "-",
        "preset": "proxy-disclosure"
      },
      {
        "op": "=",
        "name": "Authorization",
        "values": ["Bearer upstream-token"]
      },
      { "op": "=", "name": "X-Dproxy-key1", "values": ["value1"] },
      { "op": "=", "name": "X-Dproxy-key2", "values": ["value2"] },
      { "op": "=", "name": "X-Upstream-Tenant", "values": ["tenant-a"] }
    ]
  },
  "plugins": {
    "llmusage": {
      "protocol": "openai.responses",
      "labels": {
        "provider": "openai",
        "account": "primary"
      }
    },
    "llmperf": {
      "protocol": "openai.responses",
      "labels": {
        "provider": "openai"
      }
    }
  }
}
```

使用 `directive.Encode` 生成 inline token，使用 `directive.EncodeRemote` 生成 remote token。

每条 header op 必须且只能提供 `name`、`glob` 或 `preset` 之一：

- `name` 执行大小写不敏感的精确匹配，Set/Add 可以创建 header。
- `glob` 使用 Go `path.Match` 语法执行大小写不敏感的全名匹配，只影响该操作执行时已经存在的普通 header。
- `preset` 当前只接受 `proxy-disclosure`，且只支持 Remove。该预设匹配 `X-Forwarded-*` 以及常见的 forwarding、代理链和客户端地址 header。
- Glob 支持 `*`、`?`、字符类和转义，不匹配特殊的 `Host`。
- Set (`=`) 和 Add (`+`) 必须包含 `values`；Remove (`-`) 删除完整 header，不能包含 `values`。
- ops 按数组顺序执行。`patch` 继承所有端到端入站 header，不会隐式移除代理披露 header；`replace` 从空 header 集合开始。Glob 和 Preset 只匹配操作执行时已经存在的 header。

HTTP hop-by-hop header 始终按代理传输规则移除，不受 directive 控制。精确名称的 `X-Dproxy-*` header op 会被消费为请求 Metadata，支持 `=`、`+`、`-` 的顺序语义，并可由 Capture 插件输出；它们不会发送给上游。`X-Dproxy-Trace-ID` 为系统保留字段。directive 的 `plugins` 是按 attempt 生效的插件配置；引用未启用插件或提交非法插件配置会在发起上游请求前失败。携带 dproxy token 的入站 `Authorization` 会被消费；如果上游需要自己的 `Authorization`，需要通过后续 header op 显式写入。

HTTP RemoteSpec：

```json
{
  "type": "http",
  "url": "https://policy.example.com/v1/resolve",
  "key": "team-a/service-a",
  "headers": {
    "Authorization": "Bearer policy-token"
  },
  "request_headers": ["Content-Type", "X-Tenant", "X-Region-*"]
}
```

服务向该 URL 发送 `POST application/json`，不发送原请求 body：

```json
{
  "protocol": "dproxy.resolve.v1",
  "key": "team-a/service-a",
  "request": {
    "method": "POST",
    "url": "https://gateway.example.com/v1/resources?region=cn",
    "host": "gateway.example.com",
    "headers": { "Content-Type": ["application/json"] }
  }
}
```

`request_headers` 使用大小写不敏感的精确名称或 glob。默认不向 resolver 披露任何原请求 header；dproxy `Authorization` 与 hop-by-hop headers 即使被选择也不会发送。HTTP resolver 使用独立直连 transport，不读取环境代理、不跟随重定向；`200` body 是完整 directive，`204/404` 表示未找到。

Redis RemoteSpec：

```json
{
  "type": "redis",
  "url": "redis://user:password@redis.example.com:6379/1",
  "key": "dproxy:directive:team-a/service-a"
}
```

服务要求 Redis 8+，对 token 指定的 Redis URL 建立动态 client，并执行精确的 `JSON.GET key` 读取根 JSON 文档，不添加 prefix。每个 key 必须通过 `JSON.SET key $ <directive-json>` 存储完整 directive 对象；旧的 String key 不会被兼容读取。client 按连接 URL 指纹进行有界复用；directive value 不缓存。remote token 可包含连接凭据，必须按密钥处理，避免写入日志或公开配置。

```shell
redis-cli JSON.SET 'dproxy:directive:team-a/service-a' '$' \
  '{"target":{"url":"https://api.example.com"}}'
```

全局配置只限制远端解析使用的资源：

```yaml
proxy:
  directive:
    max-token-bytes: 65536
    max-inline-bytes: 49152
    remote:
      timeout: 1s
      max-response-bytes: 262144
      http:
        max-request-bytes: 131072
      redis:
        client-cache-capacity: 64
        client-idle-timeout: 10m
        pool-size: 4
```

已认证的 Control API 提供唯一的协议编解码与校验实现，Web 工作台也使用这些端点：

```text
POST /api/control/directives/encode
POST /api/control/directives/decode
POST /api/control/directives/validate
```

data-plane 错误使用 `{ "error": { "code": "...", "message": "..." } }`，客户端应依赖稳定 `code`，不要匹配文案。

## 运行

```bash
go run . server
```

默认 HTTP 监听地址是 `:23198`，可通过 `--http.listen` 修改。

常用端点：

```text
HTTP (:23198)
  GET /auth/session
  DELETE /auth/session
  POST /auth/login/token
  GET /auth/login/github
  GET /auth/callback/github
  GET /health
  PUT /api/public/retry
  GET /api/control/proxy-requests
  PUT /api/control/proxy-requests/{trace_id}/retry
  GET /api/control/openapi.json
  GET /api/control/docs
  ANY /*  (需要 Authorization: Bearer dproxy.*)
```

## 验证

```bash
go test ./...
go test -count=1 ./internal/testutil/tddcheck
```
