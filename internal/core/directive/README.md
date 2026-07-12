# `directive`

`directive` 负责解析 `Authorization: Bearer dproxy.13.<kind>.<payload>`，必要时通过 HTTP 或 Redis 读取完整 directive，并转换成 `proxy.Plan`。

面向使用者的 payload 示例和字段说明放在根目录 [README.md](../../../README.md)；这里只保留包内部维护说明，避免两处文档重复。

## 职责

- 从 `Authorization: Bearer <token>` 提取 `dproxy.` family token
- 将 dproxy family 请求与 control 请求分流，decoder 只接受 `dproxy.13.i/r` 四段格式
- inline 解码 directive JSON；remote 解码自包含 `RemoteSpec` 并通过 `RemoteReader` 读取完整 JSON
- 校验 v13 token、RemoteSpec 与 directive payload schema
- 将 target、proxy、headers 等 payload 字段组装成 `proxy.Plan`

## 处理流程

1. `resolver.go` 读取 `Authorization` bearer token。
2. 非 dproxy family token 由 proxy handler 交给下一个 HTTP handler；dproxy family token 必须是 `dproxy.13.i/r.<base64url>`。
3. `payload_codec.go` 解码来源；remote value 和 inline JSON 使用同一严格 schema，未知字段会被拒绝。
4. `assemble.go` 对 payload 做一次 normalize，并转换成 `proxy.Plan`。
5. `payload_validate.go` 保留对外校验入口，复用 normalize 流程。

## 实现约定

- payload schema 是破坏式严格协议，不做旧字段兼容。
- `dproxy.13.` 后的 `i`/`r` 明确区分 inline directive 与自包含 RemoteSpec。
- HTTP/Redis 返回值必须是完整 payload，不做合并、回退、value 缓存或递归引用。
- header op 必须且只能使用 `name`、`glob` 或 `preset` selector；Glob 使用大小写不敏感的 `path.Match` 全名匹配。
- Preset 当前只接受仅用于 Remove 的 `proxy-disclosure`；preset 是有序 op，不是隐式清理策略。
- `Host` 只接受 exact selector；Remove 删除完整 header，不接受 `values`。
- malformed 或不支持版本的 dproxy family token 返回 `proxy.ErrInvalidDirective`。
- 未识别到 dproxy family token 返回 `proxy.ErrNoMatch`，不会启动代理请求生命周期。

## 文件结构

- `resolver.go`: Authorization bearer directive token 提取和统一解析
- `payload.go`: payload schema
- `payload_codec.go`: dproxy.13 i/r token、RemoteSpec 编解码和严格 JSON 解码
- `payload_validate.go`: payload 字段校验入口
- `assemble.go`: payload normalize -> `proxy.Plan`
