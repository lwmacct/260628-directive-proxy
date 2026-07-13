# `directive`

`directive` 负责解析 `Authorization: Bearer dproxy.14.<kind>.<payload>`，必要时通过 HTTP 或 Redis 读取完整 directive，并转换成 `proxy.Plan`。

面向使用者的 payload 示例和字段说明放在根目录 [README.md](../../../README.md)；这里只保留包内部维护说明，避免两处文档重复。

远程来源的信任前提、适配器边界和禁止方向见 [Directive 远程适配器设计约束](../../../docs/directive-remote-adapter-design.md)。修改 remote token、HTTP 或 Redis adapter 前应先核对该文档。

## 职责

- 从 `Authorization: Bearer <token>` 提取 `dproxy.` family token
- 将 dproxy family 请求与 control 请求分流，decoder 只接受 `dproxy.14.i/r` 四段格式
- inline 解码 directive JSON；remote 解码自包含 `RemoteSpec` 并通过 `RemoteReader` 读取完整 JSON
- 校验 v14 token、RemoteSpec 与 directive payload schema
- 将 target、proxy、headers 等 payload 字段组装成 `proxy.Plan`

## 处理流程

1. `resolver.go` 读取 `Authorization` bearer token。
2. 非 dproxy family token 由 proxy handler 交给下一个 HTTP handler；dproxy family token 必须是 `dproxy.14.i/r.<base64url>`。
3. `payload_codec.go` 将 token 完整解码为领域 `Document`；inline payload 和 remote spec 在返回前已经校验。
4. remote document 由 `RemoteReader` 取得裸 payload JSON，再进入与 inline 相同的严格解码流程。
5. `assemble.go` 将合法 payload 直接编译成 `proxy.Plan`；resolver 另行返回来源观测信息。

## 实现约定

- payload schema 是破坏式严格协议，不做旧字段兼容。
- `dproxy.14.` 后的 `i`/`r` 明确区分 inline directive 与自包含 RemoteSpec。
- HTTP RemoteSpec 默认不披露原请求 header，只有 `request_headers` 显式选择的 header 才会发送给 resolver。
- HTTP 返回体和 Redis 8+ JSON 根文档必须是完整 payload，不做合并、回退、value 缓存或递归引用。
- Redis directive 只使用 `JSON.GET key` 读取根文档；String key 不兼容，由写入方使用 `JSON.SET key $` 管理。
- header op 必须且只能使用 `name`、`glob` 或 `preset` selector；Glob 使用大小写不敏感的 `path.Match` 全名匹配。
- Preset 当前只接受仅用于 Remove 的 `proxy-disclosure`；preset 是有序 op，不是隐式清理策略。
- `Host` 只接受 exact selector；Remove 删除完整 header，不接受 `values`。
- malformed 或不支持版本的 dproxy family token 返回 `proxy.ErrInvalidDirective`。
- 未识别到 dproxy family token 返回 `proxy.ErrNoMatch`，不会启动代理请求生命周期。

## 文件结构

- `resolver.go`: Authorization bearer directive token 提取、远端读取和统一编排
- `payload.go`: `Document`、payload 和 `RemoteSpec` schema
- `payload_codec.go`: 完整 `Document` 编解码、规范化和严格 JSON 解码
- `payload_validate.go`: payload 字段校验入口
- `assemble.go`: payload -> `proxy.Plan`

远端实现按 adapter 分离：

- `internal/adapter/directive/remote/http`: HTTP resolver 协议与 transport
- `internal/adapter/directive/remote/redis`: Redis JSON 读取与动态 client cache
- `internal/appcmd/server/directive_remote.go`: 在组合根按 `RemoteSpec.type` 分派 adapter
