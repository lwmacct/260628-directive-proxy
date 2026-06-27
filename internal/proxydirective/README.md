# `proxydirective`

`proxydirective` 负责解析 `Authorization: Bearer dpx1.<payload>` 中的 v1 directive token，并把它转换成 `proxy.Plan`。

面向使用者的 payload 示例和字段说明放在根目录 [README.md](../../README.md)；这里只保留包内部维护说明，避免两处文档重复。

## 职责

- 只从 `Authorization: Bearer <token>` 提取 directive token
- 只识别 `dpx1.` 前缀；非 `dpx1.` bearer token 不尝试解码
- 解码 base64url JSON 并校验 v1 payload schema
- 将 target、transport proxy、headers 等 payload 字段组装成 `proxy.Plan`

## 处理流程

1. `resolver.go` 读取 `Authorization` bearer token。
2. bearer token 必须以 `dpx1.` 开头，否则返回 `proxy.ErrInvalidPlan`。
3. `payload_codec.go` 解码 base64url 并使用严格 JSON schema，未知字段会被拒绝。
4. `assemble.go` 对 payload 做一次 normalize，并转换成 `proxy.Plan`。
5. `payload_validate.go` 保留对外校验入口，复用 normalize 流程。

## 实现约定

- payload schema 是破坏式严格协议，不做旧字段兼容。
- payload 必须包含 `version: 1` 和 `kind: "directive-proxy.directive"`。
- malformed `dpx1.` token 返回 `proxy.ErrInvalidDirective`。
- 未识别到 directive token 返回 `proxy.ErrInvalidPlan`，由 data plane 响应缺失 directive。

## 文件结构

- `resolver.go`: Authorization bearer directive token 提取和统一解析
- `payload.go`: payload schema
- `payload_codec.go`: dpx1 token 编解码和严格 JSON 解码
- `payload_validate.go`: payload 字段校验入口
- `assemble.go`: payload normalize -> `proxy.Plan`
