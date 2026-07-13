# 260628-directive-proxy

`260628-directive-proxy`（Directive Proxy）是由 directive token 驱动的通用 HTTP 反向代理 data plane。

`dproxy` 是 Directive Proxy 的协议前缀，当前用于 `dproxy.14.*` directive token 和 `dproxy.resolve.v1` 远端解析协议。

项目只负责解析 `Authorization: Bearer dproxy.14...` 中的 directive，按 directive 改写请求并转发到目标上游。

服务仅使用一个 HTTP listener，默认监听 `:23198`：

- 携带 `Authorization: Bearer dproxy.*` 且通过来源白名单的请求进入基于原生 `net/http` 的反向代理。
- 其他请求进入 control handler，包括 Huma `/api/*`、`/health` 和可选的 Web UI。

Authorization 分流优先于路径，因此携带 dproxy token 的 `/api/*` 请求仍会进入代理。代理流量不经过 Huma，避免流式响应、请求体和上游 header 被 API 框架额外处理。

Control API 支持 Dex OIDC 和静态 Access token 两种认证模式。`/api/*` 必须通过当前模式认证；`/health` 和 Web UI 不受 Directive 来源白名单影响。dproxy 代理流量在解析 token 或访问远端 resolver 前先执行来源校验。

## Control API 登录

`server.http.auth.methods` 可包含 `oidc`、`token` 或同时包含两者，默认只启用 `token`。只有启用的认证配置会在启动时校验和初始化。

同时启用时，浏览器登录页以 Access token 表单为主体，并提供可选的 GitHub 登录按钮。Control API 接受任一方式：有 Bearer 头或有效 Token Cookie 时使用 tokenauth，否则使用 OIDC；无效 Bearer 不会降级为 OIDC Cookie。

### OIDC 模式

启用 OIDC 时连接 Dex，使用 public client、Authorization Code Flow 和 S256 PKCE：

```yaml
server:
  http:
    auth:
      methods:
        - oidc
      oidc:
        issuer: https://2008.s.lwmacct.com:20088
        client-id: dproxy
        external-urls:
          - http://localhost:23199
        allowed-users:
          - lwmacct
        session-ttl: 24h
```

`allowed-users` 对 Dex `preferred_username` 中的 GitHub 用户名执行忽略大小写的精确匹配。服务仍验证 `federated_claims.connector_id == github` 并保留 GitHub 数字用户 ID，用于身份响应、头像和审计日志；数字 ID 不参与本地授权配置。

登录成功后，`oidcauth` 包将 Dex ID Token 保存为 HttpOnly Cookie。每次 API 请求都会重新验证 issuer、audience、签名、有效期、GitHub connector 和本地管理员配置；服务不保存 GitHub access token，也不维护本地 Session 数据库。

生产部署必须为每个工具注册独立 Dex client，并配置 HTTPS `external-urls`。OIDC callback 固定由每个 origin 派生为 `<external-url>/oidcauth/callback`，且必须全部注册到 Dex client。服务按请求 Host 精确选择 origin；不同域名各自持有 Host-only Cookie。默认值指向本地 Vite 的 `http://localhost:23199`；运行打包后的单端口服务时将它改为 `http://localhost:23198`。

### Access token 模式

不部署 Dex 时，只需生成一个至少 32 字节的随机 token：

```shell
openssl rand -base64 32
```

通过环境变量注入 token，避免把凭据提交到仓库：

```shell
export API_ACCESS_TOKEN="$(openssl rand -base64 32)"
```

默认配置会读取该环境变量；未设置或值为空时，服务会拒绝启动。显式配置方式如下：

```yaml
server:
  http:
    auth:
      methods:
        - token
      token:
        tokens:
          - "${API_ACCESS_TOKEN}"
        secure-cookie: true
```

浏览器在登录页输入 token 后，`tokenauth` 包将其保存为 HttpOnly、SameSite=Strict 的浏览器会话 Cookie。服务在每次请求时重新比对当前配置；从 `tokens` 删除凭据会立即撤销对应登录，不需要 Session 数据库。HTTPS 由本服务终止时会自动启用 Secure Cookie；HTTPS 在反向代理终止时必须显式设置 `secure-cookie: true`。

自动化客户端无需调用登录端点，可直接访问 Control API：

```http
Authorization: Bearer <access-token>
```

配置支持多个 token，便于无中断轮换。token 长度限制为 32-3800 字节，且只能使用适合 HTTP Bearer 与 Cookie 的可见 ASCII 字符；重复、空白、过短或包含不安全字符的 token 会导致服务拒绝启动。

同时提供两种登录方式时配置：

```yaml
server:
  http:
    auth:
      methods:
        - oidc
        - token
      oidc:
        issuer: https://auth.example.com
        client-id: dproxy
        external-urls:
          - https://proxy.example.com
        allowed-users:
          - octocat
        session-ttl: 24h
      token:
        tokens:
          - "${API_ACCESS_TOKEN}"
        secure-cookie: true
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
  GET /auth/config
  GET /oidcauth/login
  GET /oidcauth/callback
  GET /oidcauth/session
  POST /oidcauth/logout
  POST /tokenauth/login
  GET /tokenauth/session
  POST /tokenauth/logout
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
