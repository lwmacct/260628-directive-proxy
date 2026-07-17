# `directive`

`directive` 负责解析 `Authorization: Bearer dp.<version>.<inline|remote>.<base64url-json>`，必要时通过 HTTP 或 Redis 读取完整 directive，并转换成 `proxy.Plan`。

面向使用者的 payload 示例和字段说明放在根目录 [README.md](../../../README.md)；这里只保留包内部维护说明，避免两处文档重复。

远程来源的信任前提、适配器边界和禁止方向见 [Directive 远程适配器设计约束](../../../docs/directive-remote-adapter-design.md)。修改 remote token、HTTP 或 Redis adapter 前应先核对该文档。

## 职责

- 从 `Authorization: Bearer <token>` 提取 `dp.` family token
- 将 dp family 请求与保留 API 请求分流，decoder 只接受当前 `dp.<version>.inline/remote` 四段格式
- inline 第四段直接解码为 `Payload`；remote 第四段直接解码为 `RemoteSpec` 并通过 `RemoteReader` 读取同一 `Payload`
- 校验当前版本 token、RemoteSpec 与 directive payload schema
- 将 target、proxy、headers 等 payload 字段组装成 `proxy.Plan`

## 处理流程

1. `resolver.go` 读取 `Authorization` bearer token。
2. 非 dp family token 由 proxy handler 交给下一个 HTTP handler；dp family token 必须是当前 `dp.<version>.inline/remote.<base64url-json>`。
3. `payload_codec.go` 直接解码 inline `Payload` 或 remote `RemoteSpec`，不接受额外 envelope。
4. remote spec 在 Prepare 阶段由 `RemoteReader` 解引用一次，取得的 payload 进入与 inline 相同的严格解码流程。
5. `assemble.go` 将合法 payload 直接编译成 `proxy.Plan`；resolver 另行返回来源观测信息。

## 实现约定

- payload schema 是破坏式严格协议，不做旧字段兼容。
- `dp.<version>.` 后的 `inline`/`remote` 明确区分 inline directive 与自包含 RemoteSpec；实际版本只由 `TokenVersion` 定义。
- inline JSON 本身就是 Payload；remote JSON 本身就是 RemoteSpec。
- RemoteSpec 只包含读取信息，声明 payload、program、recovery 或其他执行字段必须拒绝。
- HTTP RemoteSpec 的直接请求头复用 Inline request header policy；默认 patch 原请求头，Authorization、Content-Length 和代理披露头在 ops 前清理，`x-dproxy-*` 与 hop-by-hop header 在 ops 后统一清理。
- HTTP 返回体和 Redis 8+ JSON 根文档必须是完整 payload；program 与 recovery 只属于 Payload。
- remote Payload 每个请求只读取一次，不做字段合并、Attempt 重读、回退、value 缓存或递归引用。
- Redis directive 只使用 `JSON.GET key` 读取根文档；String key 不兼容，由写入方使用 `JSON.SET key $` 管理。
- `headers` 是单一 HeaderPolicy；每条 op 必须显式声明 `side: request|response`，操作只允许 `set|del|add`，并且只能使用 `name` 或 `glob` selector；Glob 使用大小写不敏感的 `path.Match` 全名匹配。
- `mode` 和 `preserve_proxy_disclosure` 只作用于 request；请求 header 默认使用 patch 模式并移除代理披露 header。
- HTTP RemoteSpec 复用同一 HeaderPolicy，但只允许 `side: request`；旧 `direction`、符号操作、request/response 子容器、旧 `request_mode` 和缺少 side 的 op 均不兼容。
- 响应 header op 只应用于最终上游响应，不应用于被重试丢弃的响应、informational response、trailer 或本地代理错误。
- `Host` 只接受 exact selector；`del` 删除完整 header，不接受 `values`。
- malformed 或不支持版本的 dp family token 返回 `proxy.ErrInvalidDirective`。
- 未识别到 dp family token 返回 `proxy.ErrNoMatch`，不会启动代理请求生命周期。

## 文件结构

- `resolver.go`: Authorization bearer directive token 提取、远端读取和统一编排
- `payload.go`: `Document`、payload 和 `RemoteSpec` schema
- `payload_codec.go`: 完整 `Document` 编解码、规范化和严格 JSON 解码
- `payload_validate.go`: payload 字段校验入口
- `assemble.go`: payload -> `proxy.Plan`

远端实现按 adapter 分离：

- `internal/adapter/directive/remotehttp`: HTTP resolver 协议与 transport
- `internal/adapter/directive/remoteredis`: Redis JSON 读取与动态 client cache
- `internal/appcmd/server/directive_remote.go`: 在组合根实现统一 `RemoteReader` 端口，并按 `RemoteSpec.type` 适配和分派具体 reader
