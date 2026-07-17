# Directive 远程适配器设计约束

状态：已采纳

本文定义 v18 remote directive 的唯一语义：Remote 只是取得 `Payload` 的方式，不拥有任何独立的执行字段。

## 协议模型

inline token 的第四段解码后直接是 `Payload`：

```text
dp.18.inline.<base64url(Payload JSON)>
```

remote token 的第四段解码后直接是 `RemoteSpec`：

```text
dp.18.remote.<base64url(RemoteSpec JSON)>
```

统一处理流程：

```text
inline token -> Payload -------------------------┐
                                                 ├─> DecodePayload -> Validate -> ToPlan
remote token -> RemoteSpec -> RemoteReader ------┘
```

RemoteSpec 只允许描述如何读取 Payload：

```json
{
  "type": "http",
  "url": "https://resolver.example.com/v1/directive",
  "key": "team-a/service-a",
  "headers": {
    "mode": "patch",
    "ops": [
      {"side": "request", "op": "set", "name": "Authorization", "values": ["Bearer resolver-token"]}
    ]
  }
}
```

RemoteSpec 不使用 `source` 包裹，也不能声明以下字段：

- `payload`；
- `program`；
- `recovery`；
- target、proxy、业务 headers 等任何执行字段。

这些字段出现时必须作为未知字段拒绝，不做兼容或隐式迁移。

## Payload 是唯一执行模型

inline token 内容、HTTP response body 和 Redis JSON 根文档使用完全相同的 `Payload` schema。Payload 可以包含：

- target 与 join path；
- SOCKS5 proxy；
- request/response header policy；
- `program.request` 与 `program.attempt`；
- Recovery Controller、triggers 与 budget。

Payload 的 `headers` 是一个统一 HeaderPolicy，不再拆分 `request` / `response` 子对象：

```json
{
  "headers": {
    "mode": "patch",
    "preserve_proxy_disclosure": false,
    "ops": [
      {"side": "request", "op": "set", "name": "Authorization", "values": ["Bearer upstream-token"]},
      {"side": "response", "op": "del", "name": "Server"}
    ]
  }
}
```

`side` 是每条 op 的必填字段，只允许 `request` 或 `response`；`op` 只允许 `set`、`del` 或 `add`。`mode` 与 `preserve_proxy_disclosure` 只影响 request 基线；response 没有独立 mode。旧 `direction`、符号操作、`headers.request`、`headers.response`、`request_mode` 以及缺少 side 的 op 都按非法协议拒绝。

remote resolver 返回示例：

```json
{
  "target": {"url": "https://api.example.com/v1"},
  "program": {
    "request": [
      {"id": "capture", "module": "builtin.capture", "config": {}}
    ],
    "attempt": [
      {"id": "usage", "module": "builtin.llmusage", "config": {"protocol": "openai.responses"}}
    ]
  },
  "recovery": {
    "controller": {"url": "https://controller.example.com/recovery"},
    "triggers": {"transport_error": true},
    "budget": {"max_attempts": 3}
  }
}
```

不存在 token envelope 与远端 Payload 的字段合并，也不存在优先级规则。Remote 解引用成功后，后续编译和执行不再区分 Payload 来自 inline、HTTP 还是 Redis。

## 读取生命周期

RemoteSpec 在 `Prepare` 阶段解引用一次。得到的 Payload 会被严格解码、规范化并编译为当前请求的不可变计划；各 Attempt 从该计划克隆独立副本。

这意味着：

- request-scope program 能在读取请求正文前正确初始化；
- Recovery policy 在第一个 Attempt 前已经确定；
- 同一请求不会因远端内容在 Attempt 之间变化而产生隐式配置漂移；
- Recovery retry 重放相同的已解析 Payload，不会重新读取远端值；
- 不提供旧 plan 回退、字段 merge、递归 remote token 或跨请求 value cache。

如果业务需要下一次请求使用新配置，应先更新远端 Payload，再发起新的代理请求。

## 适配器边界

core 通过统一端口读取远端内容：

```go
type RemoteReader interface {
    Read(context.Context, RemoteSpec, *http.Request) ([]byte, error)
}
```

HTTP reader 可以使用清理和改写后的直接请求头以及有限请求元数据生成 Payload；Redis reader 只按 URL 和 key 读取 JSON。adapter 只返回原始 JSON 字节，不应：

- 理解或修改 Payload 字段；
- 组装 `proxy.Plan`；
- 增加存储专属 envelope；
- 返回另一条 remote token；
- 提供缺省 Payload 或读取失败回退；
- 缓存 directive value。

组合根 `internal/appcmd/server` 负责按 `RemoteSpec.type` 分派 HTTP/Redis reader，并统一管理连接资源。core 不依赖具体 adapter。

## HTTP 适配器

HTTP adapter 向 `RemoteSpec.url` 发起请求，并把成功 response body 原样交给 directive core。

- URL 只允许 HTTP/HTTPS 且不能包含 userinfo；
- `headers` 直接复用 Payload HeaderPolicy 的 `mode`、`preserve_proxy_disclosure` 与 `ops`；
- HTTP RemoteSpec 的 op 只允许 `side: request`，因为该策略只改写 resolver 请求头；response 或缺少 side 都无效；
- patch 模式以原入站 header 为基线，replace 模式从空集合开始；
- directive Authorization 和旧 Content-Length 在 ops 前移除，ops 可重新设置 resolver Authorization；
- 代理披露 header 默认在 ops 前移除；`x-dproxy-*` 和 hop-by-hop header 在 ops 后统一移除；
- resolver metadata JSON 只包含 method、URL 和 host，不再复制 headers；
- `request_headers` 字段已删除，出现时按未知字段拒绝；
- `204/404` 表示 not found；其他非成功响应表示 unavailable 或 invalid。

## Redis 适配器

Redis 运行前提是 Redis 8+。每条 Payload 使用 Redis JSON 保存，并从根文档读取：

```shell
redis-cli JSON.SET 'dp:directive:team-a/service-a' '$' \
  '{"target":{"url":"https://api.example.com/v1"}}'

redis-cli JSON.GET 'dp:directive:team-a/service-a'
```

Redis adapter：

- 只执行 `JSON.GET key`；
- 不兼容普通 String key；
- 不添加 key prefix；
- 不解释 JSONPath；
- 可以缓存动态 Redis client，但不能缓存 Payload value。

合法 JSON 不等于合法 Payload。缺少 `target.url`、包含未知字段或字段组合错误仍由统一 `DecodePayload` 拒绝。

## 错误语义

| 场景 | 语义 |
| --- | --- |
| HTTP `204/404` 或 Redis key 不存在 | remote directive not found |
| 连接失败、超时、认证失败 | remote directive unavailable |
| 响应或文档超过大小限制 | remote directive invalid |
| Redis key 类型不是 JSON | remote directive invalid |
| Payload JSON/schema 不合法 | remote directive invalid |

错误和日志不得暴露 RemoteSpec 凭据、token 原文或远端 Payload 内容。

## 变更审查清单

- inline body 是否仍直接等于 Payload？
- remote body 是否仍直接等于 RemoteSpec？
- RemoteSpec 是否只包含读取信息？
- HTTP 与 Redis 是否返回完全相同的 Payload schema？
- Header op 是否始终显式声明 side 并使用 `set|del|add`，且 HTTP RemoteSpec 是否只包含 request op？
- program 和 recovery 是否只存在于 Payload？
- 是否意外引入了字段 merge、递归 remote、默认回退或 value cache？
- Remote 是否仍在 Prepare 阶段只解引用一次？
- Redis 是否仍只读取 JSON 根文档并拒绝 String key？
