# `proxy`

`proxy` 是 dp 的代理执行核心，负责保存已编译的 `PreparedDirective`，并把其中固定的 `Plan` 应用到 `httputil.ReverseProxy`。

## 职责

- 定义 `PreparedDirective`、`Plan`、`Resolver`、header 操作和代理错误。
- resolver 只执行一次；`ErrNoMatch` 请求直接交给可选的下一个 HTTP handler。
- `PreparedDirective` 固定持有 Source、Plan、Program、Recovery 和 Metadata；Plan 只拥有 HTTP 执行字段，后续 RoundTrip 不重新解析或比较 Plan。
- 使用 resolver 已编译的最终上游 URL，应用请求 header 基线策略，并按顺序执行 exact 和 glob header rewrite。
- 在最终上游响应写回客户端前应用 response header rewrite，同时保护连接级、framing 和 dp 系统 header。
- `patch` 从原始入站请求重建端到端 header，同时始终剥离 HTTP hop-by-hop header。
- 按 directive 中的 SOCKS5 配置选择 per-request upstream proxy。
- HTTPS upstream 显式启用并优先协商 HTTP/2，服务端不支持时回退 HTTP/1.1；明文 HTTP 不隐式启用 h2c。
- 保持 data plane 使用原生 `net/http`，避免影响流式响应。

业务协议解析不放在这里；`internal/core/directive` 负责把 inline `Payload` 或 remote `RemoteSpec -> Payload` 编译成 `PreparedDirective`。只有 resolver 匹配的请求才会进入 observer 或执行反向代理。
