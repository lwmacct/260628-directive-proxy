# `proxy`

`proxy` 是 dproxy 的代理执行核心，负责把已解析好的 `Plan` 应用到 `httputil.ReverseProxy`。

## 职责

- 定义 `Plan`、`Resolver`、header 操作和代理错误。
- resolver 只执行一次；`ErrNoMatch` 请求直接交给可选的下一个 HTTP handler。
- 拼接上游 URL，应用 header rewrite，剥离代理泄露 header 和运行时 header。
- 按 directive 中的 SOCKS5 配置选择 per-request upstream proxy。
- 保持 data plane 使用原生 `net/http`，避免影响流式响应。

业务协议解析不放在这里；`internal/core/directive` 负责把 `dproxy.10` payload 转成 `Plan`。只有 resolver 匹配的请求才会进入 observer 或执行反向代理。
