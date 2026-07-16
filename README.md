# 260628-directive-proxy

`260628-directive-proxy`（Directive Proxy）是由 directive token 驱动的通用 HTTP 反向代理 data plane。

`dproxy` 是 Directive Proxy 的协议前缀，当前用于 `dproxy.<version>.*` directive token 和 `dproxy.resolve.v1` 远端解析协议。

项目只负责解析 `Authorization: Bearer dproxy.<version>...` 中的 directive，按 directive 改写请求并转发到目标上游。

服务仅使用一个 HTTP listener，默认监听 `:23198`：

- 携带 `Authorization: Bearer dproxy.*` 且通过来源白名单的请求进入基于原生 `net/http` 的反向代理。
- `/api/public/*` 进入匿名请求协作 API，`/api/admin/*` 进入受认证保护的管理 API。
- 其他请求进入 `/health`、认证端点或可选 Web UI。

`/api/public/*` 和 `/api/admin/*` 是系统保留前缀，优先于 dproxy token 分流；其他路径（包括普通 `/api/...`）携带 dproxy token 时仍可进入代理。代理流量不经过 Huma，避免流式响应、请求体和上游 header 被 API 框架额外处理。

## Exchange 与外部介入重试

代理把每个进入 data plane 的入站请求及其完整下游响应建模为一个 `Exchange`，生成 canonical UUIDv7 `trace_id` 并通过响应头 `X-Dproxy-Trace-ID` 返回。一个 Exchange 可以包含多个上游 `Attempt`；外部 API 介入会取消当前尚未收到最终响应头的 Attempt，并从同一份不可变内存正文启动下一次 Attempt。Remote directive 在每个 Attempt 都重新读取和编译，不合并或回退旧 plan。

Proxy Retry 是每个 Exchange 的固有能力；入站请求无需携带 retry identity 即可正常转发。需要通过 Public API 介入自己的 Exchange 时，调用方携带一个 canonical UUIDv7 `Dproxy-Retry-ID`。该 header 是 bearer retry credential，代理在任何 Module、日志或上游处理前移除它，只保存 SHA-256 verifier：

- `PUT /api/public/retry`：无需 Control Auth，但必须携带 `Dproxy-Retry-ID: <uuidv7>` 和 `If-Match: "attempt:<current_attempt>"`；服务端自动计算下一 attempt。
- `GET /api/admin/proxy-requests`：认证后列出活动请求。
- `GET /api/admin/proxy-requests/{trace_id}`：认证后读取一个活动请求。
- `PUT /api/admin/proxy-requests/{trace_id}/retry`：认证后按 trace ID 介入，并携带 `If-Match: "attempt:<current_attempt>"`。

重复提交同一个已接受的 PUT 会返回原结果，不会再次取消 Attempt；终态结果按 `command-retention` 短期保留。收到最终响应头后，Exchange 立即退出可重试索引，但仍继续拥有流式响应生命周期，直到下游结束。`text/event-stream` 响应因此只在建立 SSE 之前可重试；已经开始传输的 SSE 不会被透明拼接或重连。POST/PATCH 只有在初始请求携带 `Idempotency-Key` 时才允许重试；代理会在所有 Attempt 强制保留原值，但上游仍需正确实现幂等语义。

请求正文使用单写入、多 reader 的流式 Replay Store。代理读取客户端 chunk 后立即提交 request Module barrier，并允许当前 Attempt 同时把同一批字节发送给上游；新的重试 Attempt 从 offset 0 重放已有前缀，追上尾部后继续等待客户端后续数据。Store 按实际字节执行单请求限额，不要求 `Content-Length`；小正文使用分段内存，超过阈值或全局内存紧张时转入匿名临时文件。最终响应头到达后禁止新 reader，正文历史在活动上传 reader 结束时立即释放，不被长 SSE 响应持续占用。

活动控制器和 cancel 句柄属于当前进程；多实例部署必须让 Admin API 命中持有原请求连接的实例，例如使用实例级管理地址或粘性路由。

```yaml
server:
  proxy:
    retry:
      max-attempts: 3
      command-retention: 1m
    body-store:
      memory-max-bytes: 536870912
      memory-per-body-bytes: 1048576
      disk-max-bytes: 8589934592
      max-body-bytes: 33554432
      chunk-bytes: 65536
      temp-dir: ${APP_DATA:-.local/data}/tmp/body-store
      body-read-timeout: 30s
  fluent:
    enabled: false
    buffer:
      max-events: 8192
      max-bytes: 67108864
```

## Directive Module 与输出

内置 Module 固定随程序注册，由 directive token 通过有序 `program.request` / `program.attempt` 选择并提供配置：

- [`builtin.capture`](docs/module-capture.md)：request-scope 请求、响应和生命周期审计；
- [`builtin.llmusage`](docs/module-llmusage.md)：attempt-scope LLM token usage 提取；
- [`builtin.llmperf`](docs/module-llmperf.md)：attempt-scope LLM 响应性能测量；
- Fluent 只控制 Sink、Queue 和连接；关闭时 Module 仍会注册、校验和执行，因此修改型 Module 不依赖可观测输出。
- 开启后通过内部有界队列投递统一 `dproxy.event.v2` Record，容量由 `fluent.buffer` 控制。

完整部署配置见 [`config/config.example.yaml`](config/config.example.yaml)，Module 架构见 [`docs/module-architecture.md`](docs/module-architecture.md)。

完整生命周期、并发边界与事件契约见 [Exchange lifecycle](docs/exchange-lifecycle.md)。

Admin API 支持 Dex OIDC 和静态 Access token 两种认证模式。`/api/admin/*` 必须通过当前模式认证；`/api/public/*`、`/health` 和 Web UI 不要求 Admin Auth。dproxy 代理流量在解析 token 或访问远端 resolver 前先执行来源校验。

## Admin API 登录

`server.http.authme.statictoken.enabled` 和 `server.http.authme.dexgithub.enabled` 分别控制两种认证方式；默认只启用静态 token。至少启用一种方式，只有启用的配置会在启动时校验和初始化。

同时启用时，浏览器登录页以 Access token 表单为主体，并提供可选的 GitHub 登录按钮。两种方式签发同一个加密浏览器 Session；显式 Bearer 无效时不会降级使用 Session Cookie。

所有模式共用可信 origin 和 Session key ring：

```yaml
server:
  http:
    authme:
      external-urls:
        - https://proxy.example.com
      session:
        keys:
          - id: primary
            secret: "${AUTHME_SESSION_KEY}"
        ttl: 24h
```

`AUTHME_SESSION_KEY` 必须是 base64url 编码的 32 字节随机值，可用 `openssl rand -base64 32 | tr '+/' '-_' | tr -d '='` 生成。第一把 key 用于写入，所有 key 均可解密，便于轮换。

Session TTL 默认由 Authme 提供，为 24 小时；需要更短或更长的生命周期时，再在 `authme.session.ttl` 显式覆盖。

### Access token 模式

`AUTHME_ACCESS_TOKEN` 是 opaque Bearer secret，不要求固定前缀、版本或编码。可以直接使用已有的
Redis ACL 密码、API key 或其他非空 token；只要不包含空白和控制字符即可。
这是破坏式变更：旧版 `dpctl.10...` token、`token-sha256` 配置和 namespace 字段不再兼容，升级后需要重新配置 token。

例如直接设置任意已有值：

```shell
export AUTHME_ACCESS_TOKEN="change-me"
```

需要新建高熵 token 时，可选用 OpenSSL：

```shell
export AUTHME_ACCESS_TOKEN="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=')"
```

同时生成 Session key：

```shell
export AUTHME_SESSION_KEY="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=')"
```

默认配置读取 `AUTHME_ACCESS_TOKEN`；未设置、为空、包含空白/控制字符或超过 4096 字节时，服务会拒绝启动。显式配置方式如下：

```yaml
server:
  http:
    authme:
      statictoken:
        enabled: true
        credentials:
          - id: admin
            name: Administrator
            token: "${AUTHME_ACCESS_TOKEN}"
```

浏览器输入 token 后，统一加密 Session 只保存 credential ID 和 token 的 SHA-256 revision，不保存原始 token；适配器认证索引同样只保存摘要。删除 credential 或轮换 token 会立即撤销对应登录，不需要 Session 数据库。

自动化客户端无需调用登录端点，可直接访问 Admin API：

```http
Authorization: Bearer <access-token>
```

配置使用带显式 ID 的 credential 列表，支持多个凭据以便审计和轮换。不同 credential 不能配置相同 token。ID 最长 56 字节，只接受小写字母、数字、`-` 和 `_`，并且必须以字母或数字开头、结尾。

### OIDC 模式

启用 OIDC 时连接 Dex，使用 public client、Authorization Code Flow 和 S256 PKCE：

```yaml
server:
  http:
    authme:
      origins:
        - http://localhost:23199
      dexgithub:
        enabled: true
        issuer: https://2008.s.lwmacct.com:20088
        client-id: dproxy
        session-ttl: 24h
      allowed-github-users:
        - lwmacct
```

`allowed-github-users` 对 Dex `preferred_username` 中的 GitHub 用户名执行忽略大小写的精确匹配。服务仍验证 `federated_claims.connector_id == github` 并保留 GitHub 数字用户 ID，用于身份响应、头像和审计日志；数字 ID 不参与本地授权配置。

登录回调验证 issuer、audience、签名、有效期、nonce、PKCE 和 GitHub connector 后签发本地 AES-256-GCM Session；Cookie 不保存 Dex ID Token 或 GitHub access token。本地管理员策略在每次请求时重新执行。

生产部署必须为每个工具注册独立 Dex client，并配置 HTTPS `origins`。OIDC callback 固定为 `<origin>/authme/callback/github`，且必须全部注册到 Dex client。服务按请求 Host 精确选择 origin；不同域名各自持有 Host-only Cookie。

当前服务只通过 Dex GitHub connector 使用标准 OIDC：provider 必须提供 OIDC discovery 和可验证的 ID Token，GitHub 身份由 Dex 转换为 OIDC claims。

同时提供两种登录方式时配置：

```yaml
server:
  http:
    authme:
      origins:
        - https://proxy.example.com
      statictoken:
        enabled: true
        credentials:
          - id: admin
            name: Administrator
            token: "${AUTHME_ACCESS_TOKEN}"
      dexgithub:
        enabled: true
        issuer: https://auth.example.com
        client-id: dproxy
        session-ttl: 24h
      allowed-github-users:
        - octocat
```

## Directive 来源白名单

`server.proxy.directive.source-access` 只保护携带 `Authorization: Bearer dproxy.*` 的 Directive 流量。Admin API、OIDC、`/health` 和 Web UI 继续使用各自的访问策略。来源白名单默认禁用；启用后仅允许 `allowed-sources` 中配置的来源。

```yaml
server:
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
Authorization: Bearer dproxy.<version>.i.<base64url-directive-json>
Authorization: Bearer dproxy.<version>.r.<base64url-remote-spec-json>
```

`<version>` 由 `internal/core/directive.TokenVersion` 定义。服务只接受当前版本的 `dproxy.<version>.i/r` 四段协议；其他 Bearer token 不会进入代理，旧版 token 不再兼容。

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
    "request": {
      "ops": [
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
    "response": {
      "ops": [
        { "op": "-", "name": "Server" },
        { "op": "-", "glob": "X-Upstream-*" },
        { "op": "=", "name": "Access-Control-Allow-Origin", "values": ["*"] }
      ]
    }
  },
  "program": {
    "request": [
      {"id": "capture", "module": "builtin.capture", "config": {}}
    ],
    "attempt": [
      {"id": "usage", "module": "builtin.llmusage", "config": {"protocol": "openai.responses"}}
    ]
  }
}
```

Module 配置示例见 [`docs/module-capture.md`](docs/module-capture.md)、[`docs/module-llmusage.md`](docs/module-llmusage.md) 和 [`docs/module-llmperf.md`](docs/module-llmperf.md)。

使用 `directive.Encode` 生成 inline token，使用 `directive.EncodeRemote` 生成 remote token。

`headers.request` 配置发往上游的请求 header，`headers.response` 配置最终写给客户端的响应 header。每条 op 必须且只能提供 `name` 或 `glob` 之一：

- `name` 执行大小写不敏感的精确匹配，Set/Add 可以创建 header。
- `glob` 使用 Go `path.Match` 语法执行大小写不敏感的全名匹配，只影响该操作执行时已经存在的普通 header。
- Glob 支持 `*`、`?`、字符类和转义，不匹配特殊的 `Host`。
- Set (`=`) 和 Add (`+`) 必须包含 `values`；Remove (`-`) 删除完整 header，不能包含 `values`。
- 请求 `mode` 省略时为 `patch`；`replace` 从空 header 集合开始。请求默认移除 `X-Forwarded-*` 以及常见 forwarding、代理链和客户端地址 header，只有 `preserve_proxy_disclosure: true` 才保留入站值。该策略在请求 ops 前执行，因此 ops 可以显式重建可信值。
- 响应不支持 `mode`。响应 ops 只应用于最终上游响应；被重试丢弃的响应、100/103 informational response、trailer 和 dproxy 本地错误不受影响。连接级、framing、Upgrade 与 dproxy 系统 header 不允许修改。
- 两侧 ops 都按数组顺序执行。Glob 只匹配操作执行时已经存在的普通 header。

HTTP hop-by-hop header 始终按代理传输规则移除，不受 directive 控制。精确名称的 `X-Dproxy-*` header op 会被消费为请求 Metadata，支持 `=`、`+`、`-` 的顺序语义，并可由 Capture Module 输出；它们不会发送给上游。`X-Dproxy-Trace-ID` 为系统保留字段。`program.request` 跨 retry，`program.attempt` 每次重新创建；数组顺序就是执行顺序。携带 dproxy token 的入站 `Authorization` 会被消费；如果上游需要自己的 `Authorization`，需要通过后续 header op 显式写入。

Remote token document（request program 属于稳定 token；远端 payload 只允许 attempt program）：

```json
{
  "source": {
    "type": "http",
    "url": "https://policy.example.com/v1/resolve",
    "key": "team-a/service-a",
    "headers": {"Authorization": "Bearer policy-token"},
    "request_headers": ["Content-Type", "X-Tenant", "X-Region-*"]
  },
  "program": {
    "request": [{"id":"capture","module":"builtin.capture","config":{}}]
  }
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

Redis remote token document：

```json
{
  "source": {
    "type": "redis",
    "url": "redis://user:password@redis.example.com:6379/1",
    "key": "dproxy:directive:team-a/service-a"
  }
}
```

服务要求 Redis 8+，对 token 指定的 Redis URL 建立动态 client，并执行精确的 `JSON.GET key` 读取根 JSON 文档，不添加 prefix。每个 key 必须通过 `JSON.SET key $ <directive-json>` 存储完整 directive 对象；旧的 String key 不会被兼容读取。client 按连接 URL 指纹进行有界复用；directive value 不缓存。remote token 可包含连接凭据，必须按密钥处理，避免写入日志或公开配置。

```shell
redis-cli JSON.SET 'dproxy:directive:team-a/service-a' '$' \
  '{"target":{"url":"https://api.example.com"}}'
```

全局配置只限制远端解析使用的资源：

```yaml
server:
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

已认证的 Admin API 提供唯一的协议编解码与校验实现，Web 工作台也使用这些端点：

```text
POST /api/admin/directives/encode
POST /api/admin/directives/decode
POST /api/admin/directives/validate
```

data-plane 错误使用 `{ "error": { "code": "...", "message": "..." } }`，客户端应依赖稳定 `code`，不要匹配文案。

## 运行

```bash
go run . server
```

配置树与 CLI 命令树一致：`app server` 对应配置根的 `server` 节点。配置文件使用完整路径（例如 `server.proxy.retry.max-attempts`），环境变量使用 `APP_SERVER_PROXY_RETRY_MAX_ATTEMPTS`，CLI 已由命令提供 `server` 上下文，因此使用 `--proxy.retry.max-attempts`。

默认 HTTP 监听地址是 `:23198`，可通过 `app server --http.listen=:23198` 修改。

常用端点：

```text
HTTP (:23198)
  GET /authme/session
  DELETE /authme/session
  POST /authme/login/token
  GET /authme/login/github
  GET /authme/callback/github
  GET /health
  PUT /api/public/retry
  GET /api/admin/proxy-requests
  PUT /api/admin/proxy-requests/{trace_id}/retry
  GET /api/admin/openapi.json
  GET /api/admin/docs
  ANY /*  (需要 Authorization: Bearer dproxy.*)
```

## 验证

```bash
go test ./...
go test -count=1 ./internal/testutil/tddcheck
```

## 开源协议

本项目采用 [Apache License 2.0](LICENSE) 开源协议。
