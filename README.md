# llm-relay-dproxy

`llm-relay-dproxy` 是面向 LLM relay 流量的指令代理 data plane。

项目只负责解析 `Authorization: Bearer dproxy.14...` 中的 directive，按 directive 改写请求并转发到目标上游。

服务仅使用一个 HTTP listener，默认监听 `:23198`：

- 携带 `Authorization: Bearer dproxy.*` 的请求进入基于原生 `net/http` 的反向代理。
- 其他请求进入 control handler，包括 Huma `/api/*`、`/health` 和可选的 Web UI。

Authorization 分流优先于路径，因此携带 dproxy token 的 `/api/*` 请求仍会进入代理。代理流量不经过 Huma，避免流式响应、请求体和上游 header 被 API 框架额外处理。

Control API 使用 Dex OIDC 登录，并在本地按 GitHub 数字用户 ID 授权。`/api/*` 必须持有有效身份 Cookie；`/health`、Web UI 和 dproxy 代理流量保持公开。

## Control API 登录

默认开发配置连接中央 Dex，使用 public client、Authorization Code Flow 和 S256 PKCE：

```yaml
server:
  http:
    oidc-auth:
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
  "key": "team-a/openai",
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
  "key": "team-a/openai",
  "request": {
    "method": "POST",
    "url": "https://relay.example.com/v1/chat?region=cn",
    "host": "relay.example.com",
    "headers": { "Content-Type": ["application/json"] }
  }
}
```

`request_headers` 使用大小写不敏感的精确名称或 glob。默认不向 resolver 披露任何原请求 header；dproxy `Authorization` 与 hop-by-hop headers 即使被选择也不会发送。HTTP resolver 使用独立直连 transport，不读取环境代理、不跟随重定向；`200` body 是完整 directive，`204/404` 表示未找到。

Redis RemoteSpec：

```json
{
  "type": "redis",
  "url": "rediss://user:password@redis.example.com:6380/1",
  "key": "dproxy:directive:team-a/openai"
}
```

服务对 token 指定的 Redis URL 建立动态 client，并执行精确的 `GET key`，不添加 prefix。client 按连接 URL 指纹进行有界复用；directive value 不缓存。remote token 可包含连接凭据，必须按密钥处理，避免写入日志或公开配置。

全局配置只限制远端解析使用的资源：

```yaml
proxy:
  directive:
    max-token-bytes: 65536
    max-inline-bytes: 49152
    remote:
      timeout: 1s
      max-request-bytes: 131072
      max-response-bytes: 262144
      redis-client-cache-capacity: 64
      redis-client-idle-timeout: 10m
      redis-pool-size: 4
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
  GET /oidcauth/login
  GET /oidcauth/callback
  GET /oidcauth/session
  POST /oidcauth/logout
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
