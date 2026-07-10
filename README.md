# llm-relay-dproxy

`llm-relay-dproxy` 是面向 LLM relay 流量的指令代理 data plane。

项目只负责解析 `Authorization: Bearer dproxy.10...` 中的 directive，按 directive 改写请求并转发到目标上游。

服务仅使用一个 HTTP listener，默认监听 `:23198`：

- 携带 `Authorization: Bearer dproxy.*` 的请求进入基于原生 `net/http` 的反向代理。
- 其他请求进入 control handler，包括 Huma `/api/*`、`/health` 和可选的 Web UI。

Authorization 分流优先于路径，因此携带 dproxy token 的 `/api/*` 请求仍会进入代理。代理流量不经过 Huma，避免流式响应、请求体和上游 header 被 API 框架额外处理。

## Directive Token

唯一入口是：

```http
Authorization: Bearer dproxy.10.<base64url-json>
```

`dproxy.` token family 由代理保留，当前只接受 `dproxy.10.` 协议。其他 Bearer token 不会进入代理。

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
      { "op": "=", "name": "X-Tenant", "values": ["tenant-a"] }
    ]
  }
}
```

使用 `directive.Encode` 可以生成完整的 `dproxy.10.` token。

directive 被接受后，入站 `Authorization` 会在转发前移除。如果上游需要自己的 `Authorization`，需要通过 directive 的 header ops 显式写入。

## 运行

```bash
go run . server
```

默认 HTTP 监听地址是 `:23198`，可通过 `--http.listen` 修改。

常用端点：

```text
HTTP (:23198)
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
