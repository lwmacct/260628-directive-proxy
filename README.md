# 260628-directive-proxy

`260628-directive-proxy`（Directive Proxy）是由 directive token 驱动的通用 HTTP 反向代理 data plane。

`dproxy` 是 Directive Proxy 的协议前缀，当前用于 `dproxy.14.*` directive token 和 `dproxy.resolve.v1` 远端解析协议。

项目只负责解析 `Authorization: Bearer dproxy.14...` 中的 directive，按 directive 改写请求并转发到目标上游。

服务仅使用一个 HTTP listener，默认监听 `:23198`：

- 携带 `Authorization: Bearer dproxy.*` 且通过来源白名单的请求进入基于原生 `net/http` 的反向代理。
- 其他请求进入 control handler，包括 Huma `/api/*`、`/health` 和可选的 Web UI。

Authorization 分流优先于路径，因此携带 dproxy token 的 `/api/*` 请求仍会进入代理。代理流量不经过 Huma，避免流式响应、请求体和上游 header 被 API 框架额外处理。

## 等待响应请求与人工重试

代理为每个进入 data plane 的逻辑请求生成 128-bit `trace_id`，并通过响应头 `X-Dproxy-Trace-ID` 返回。一次逻辑请求可以包含多个上游 attempt；人工重试会取消当前尚未收到最终响应头的 attempt，并使用磁盘临时文件中的相同请求正文启动下一次 attempt。directive 只解析一次，重试不会重新选择目标。

Control API：

- `GET /api/proxy-requests/awaiting-response`：列出已经发起上游请求但尚未收到最终响应头的请求。
- `GET /api/proxy-requests/{trace_id}`：读取一个仍然活动的请求。
- `POST /api/proxy-requests/{trace_id}/retry`：请求体为 `{"expected_attempt": 1}`，以 compare-and-swap 方式触发重试。

收到最终响应头后，请求立即退出可重试集合。`text/event-stream` 响应因此只在建立 SSE 之前可重试；已经开始传输的 SSE 不会被透明拼接或重连。POST 等非幂等请求的重试是 at-least-once，上游可能已经执行了第一次请求但响应尚未返回。

活动控制器和 cancel 句柄属于当前进程；多实例部署必须让 Control API 命中持有原请求连接的实例，例如使用实例级管理地址或粘性路由。

```yaml
proxy:
  retry:
    enabled: true
    retryable-after: 10s
    max-attempts: 3
    max-active-requests: 4096
    temp-dir: ""
    max-body-bytes: 33554432
    max-inflight-bytes: 1073741824
```

## Fluentd 生命周期 capture

Capture 不在进程内保留历史记录，也不提供历史查询 API。请求头、请求正文 chunk、attempt、响应头、响应正文 chunk、SSE 语义事件和完成状态都作为独立 Forward 记录同步发送到 Fluentd，并通过 `trace_id`、`attempt_id`、`record_id` 和请求内递增 `sequence` 关联。

默认 tag 为 `dproxy.capture.lifecycle`、`request.headers`、`request.body`、`attempt`、`response.headers`、`response.body` 和 `response.sse`。正文使用 Base64 chunk、绝对 offset 和 SHA-256 end 记录，SSE 同时保存原始响应字节和解析后的每条 event/comment。

Exporter 固定使用同步模式，避免 Fluent Logger 的进程内异步队列；建议连接本机 Unix socket，并由 Fluentd 使用文件 buffer。启用 capture 时启动阶段无法连接 Fluentd 会导致服务启动失败；运行阶段发送失败采用 fail-open，代理继续处理请求，并在 `/health` 的 `capture` 字段报告 degraded 状态。

完整事件契约与部署约束见 [Proxy request lifecycle](docs/proxy-request-lifecycle.md)。

Control API 支持 Dex OIDC 和静态 Access token 两种认证模式。`/api/*` 必须通过当前模式认证；`/health` 和 Web UI 不受 Directive 来源白名单影响。dproxy 代理流量在解析 token 或访问远端 resolver 前先执行来源校验。

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
Authorization: Bearer dproxy.14.i.<base64url-directive-json>
Authorization: Bearer dproxy.14.r.<base64url-remote-spec-json>
```

`dproxy.` token family 由代理保留，当前只接受 `dproxy.14.i` 和 `dproxy.14.r` 四段协议。其他 Bearer token 不会进入代理，旧版 token 不再兼容。

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

HTTP hop-by-hop header 始终按代理传输规则移除，不受 directive 控制。所有 `X-Dproxy-*` header 也会在出站前无条件移除，即使 header op 尝试写入也不会发送给上游。directive 被接受后，携带 dproxy token 的入站 `Authorization` 也会被消费；如果上游需要自己的 `Authorization`，需要通过后续 header op 显式写入。

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
POST /api/directives/encode
POST /api/directives/decode
POST /api/directives/validate
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
  GET /api/health
  GET /api/openapi.json
  GET /api/docs
  ANY /*  (需要 Authorization: Bearer dproxy.*)
```

## 验证

```bash
go test ./...
go test -count=1 ./internal/testutil/tddcheck
```
