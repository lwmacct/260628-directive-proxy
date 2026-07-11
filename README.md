# llm-relay-dproxy

`llm-relay-dproxy` 是面向 LLM relay 流量的指令代理 data plane。

项目只负责解析 `Authorization: Bearer dproxy.11...` 中的 directive，按 directive 改写请求并转发到目标上游。

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
    auth:
      issuer: https://2008.s.lwmacct.com:20088
      client-id: dproxy-local
      callback-url: http://localhost:23198/auth/callback
      public-url: http://localhost:23199
      allowed-users:
        - lwmacct
      session-ttl: 24h
```

`allowed-users` 对 Dex `preferred_username` 中的 GitHub 用户名执行忽略大小写的精确匹配。服务仍验证 `federated_claims.connector_id == github` 并保留 GitHub 数字用户 ID，用于身份响应、头像和审计日志；数字 ID 不参与本地授权配置。

登录成功后，服务将 Dex ID Token 保存为 HttpOnly Cookie。每次 API 请求都会重新验证 issuer、audience、签名、有效期、GitHub connector 和本地管理员配置；服务不保存 GitHub access token，也不维护本地 Session 数据库。

生产部署必须为每个工具注册独立 Dex client，并配置 HTTPS `callback-url` 和 `public-url`。默认 `public-url` 指向本地 Vite 的 `http://localhost:23199`；运行打包后的单端口服务时将它改为 `http://localhost:23198`。

## Directive Token

唯一入口是：

```http
Authorization: Bearer dproxy.11.<base64url-json>
```

`dproxy.` token family 由代理保留，当前只接受 `dproxy.11.` 协议。其他 Bearer token 不会进入代理。

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
        "op": "=",
        "name": "Authorization",
        "values": ["Bearer upstream-token"]
      },
      { "op": "-", "glob": "M-Runtime-*" },
      { "op": "=", "glob": "X-Tenant-*", "values": ["tenant-a"] }
    ]
  }
}
```

使用 `directive.Encode` 可以生成完整的 `dproxy.11.` token。

每条 header op 必须且只能提供 `name` 或 `glob`：

- `name` 执行大小写不敏感的精确匹配，Set/Add 可以创建 header。
- `glob` 使用 Go `path.Match` 语法执行大小写不敏感的全名匹配，只影响该操作执行时已经存在的普通 header。
- Glob 支持 `*`、`?`、字符类和转义，不匹配特殊的 `Host`。
- Set (`=`) 和 Add (`+`) 必须包含 `values`；Remove (`-`) 删除完整 header，不能包含 `values`。
- ops 按数组顺序执行。`replace` 模式从空 header 集合开始，因此 Glob 只能匹配前序 op 创建的 header。

directive 被接受后，入站 `Authorization` 会在转发前移除。如果上游需要自己的 `Authorization`，需要通过 directive 的 header ops 显式写入。

## 运行

```bash
go run . server
```

默认 HTTP 监听地址是 `:23198`，可通过 `--http.listen` 修改。

常用端点：

```text
HTTP (:23198)
  GET /auth/login
  GET /auth/callback
  GET /auth/session
  POST /auth/logout
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
