# `directive`

`directive` 负责解析 `Authorization: Bearer dp.19.<inline|remote>.<base64url-json>.<hmac>`，先校验 TokenSecret 的 HMAC，再按需通过 HTTP、Redis 或 File 读取完整 directive，并转换成 `proxy.Plan`。

面向使用者的 payload 示例和字段说明放在根目录 [README.md](../../../README.md)；这里只保留包内部维护说明，避免两处文档重复。

远程来源的信任前提、适配器边界和禁止方向见 [Directive 远程适配器设计约束](../../../docs/directive-remote-adapter-design.md)。修改 remote token、HTTP、Redis 或 File adapter 前应先核对该文档。

## 职责

- 从 `Authorization: Bearer <token>` 提取 `dp.` family token
- 将 dp family 请求与保留 API 请求分流，decoder 只接受当前 `dp.19.<kind>.<base64url-json>.<hmac>` 五段格式并校验 HMAC
- inline 第四段直接解码为 `Payload`；remote 第四段直接解码为 `RemoteSpec`，编译为 typed reference 后通过 HTTP/Redis/File reader 读取同一 `Payload`
- 校验当前版本 token、RemoteSpec 与 directive payload schema
- 将 target、proxy、headers 等 payload 字段组装成 `proxy.Plan`

## 处理流程

1. `resolver.go` 读取 `Authorization` bearer token。
2. 非 dp family token 由 proxy handler 交给下一个 HTTP handler；dp family token 必须是当前 `dp.<version>.inline/remote.<base64url-json>`。
3. `payload_codec.go` 直接解码 inline `Payload` 或 remote `RemoteSpec`，不接受额外 envelope。
4. remote spec 在 Prepare 阶段编译为 `HTTPReference`、`RedisReference` 或 `FileReference`，并由对应 reader 解引用一次；取得的 payload 进入与 inline 相同的严格解码流程。
5. `assemble.go` 将合法 payload 直接编译成 `proxy.Plan`；resolver 另行返回来源观测信息。

## 实现约定

- payload schema 是破坏式严格协议，不做旧字段兼容。
- `dp.19.<kind>.<base64url-json>.<hmac>` 明确区分 inline directive 与自包含 RemoteSpec；实际版本只由 `TokenVersion` 定义。
- inline JSON 本身就是 Payload；remote JSON 本身就是 RemoteSpec。
- RemoteSpec 只包含读取信息，声明 payload、program、recovery 或其他执行字段必须拒绝。
- RemoteSpec 顶层必须且只能包含 `http`、`redis`、`file` 之一，不使用共享 `type` 和跨 backend 可选字段。
- HTTP RemoteSpec 的直接请求头复用 Inline request header policy；默认 patch 原请求头，Authorization、Content-Length 和代理披露头在 mutations 前清理，`x-dproxy-*` 与 hop-by-hop header 在 mutations 后统一清理。
- HTTP 返回体、Redis 8+ JSON 根文档和 File 文件必须是完整 payload；program 与 recovery 只属于 Payload。
- remote Payload 每个请求只读取一次，不做字段合并、Attempt 重读、回退、value 缓存或递归引用。
- Redis directive 只使用 `JSON.GET key` 读取根文档；String key 不兼容，由写入方使用 `JSON.SET key $` 管理。
- File directive 只使用 slash 相对 `path`，由配置 root 限定读取范围；支持子目录，只读取普通文件。
- `headers` 是单一 HeaderPolicy；每条 mutation 必须显式声明 `side: request|response`，action 只允许 `set|remove|append`，并且只能使用 `name` 或 `glob` selector；Glob 使用大小写不敏感的 `path.Match` 全名匹配。
- `mode` 和 `preserve_proxy_disclosure` 只作用于 request；请求 header 默认使用 patch 模式并移除代理披露 header。
- HTTP RemoteSpec 复用同一 HeaderPolicy，但只允许 `side: request`。
- 响应 header mutation 只应用于最终上游响应，不应用于被重试丢弃的响应、informational response、trailer 或本地代理错误。
- `Host` 只接受 exact selector；`remove` 删除完整 header，不接受 `values`。
- malformed 或不支持版本的 dp family token 返回 `proxy.ErrInvalidDirective`。
- 未识别到 dp family token 返回 `proxy.ErrNoMatch`，不会启动代理请求生命周期。

## 文件结构

- `resolver.go`: Authorization bearer directive token 提取、远端读取和统一编排
- `payload.go`: `Document`、payload 和 `RemoteSpec` schema
- `payload_codec.go`: 完整 `Document` 编解码、规范化和严格 JSON 解码
- `payload_validate.go`: payload 字段校验入口
- `assemble.go`: payload -> `proxy.Plan`
- `remote.go`: RemoteSpec -> typed reference、请求快照和 HTTP/Redis/File reader 端口

远端实现按 adapter 分离：

- `internal/core/httpheader`: proxy 与 HTTP resolver 共用的 header plan 和执行语义
- `internal/adapter/directivehttp`: HTTP resolver 协议与 transport
- `internal/adapter/directiveredis`: Redis JSON 读取与动态 client cache
- `internal/adapter/directivefile`: 配置根目录内的直接文件读取
- `internal/appcmd/server/directive_wiring.go`: 在组合根注入三个 reader 并统一管理资源
