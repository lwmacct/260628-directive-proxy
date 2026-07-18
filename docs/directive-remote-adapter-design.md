# Directive 远程适配器设计约束

状态：已采纳

本文定义 v21 remote directive 的唯一语义：Remote 只是取得 `Payload` 的方式，不拥有任何独立的执行字段。

RemoteSpec 是可信 directive 的一部分，并拥有最高优先级。动态 endpoint、Redis 连接信息、认证信息和 header policy 都是 `directive proxy` 的预期控制能力，不由 adapter 改写为服务端命名 source、allowlist 或其他间接引用。

## 协议模型

inline token 的第四段解码后直接是 `Payload`：

```text
dp.21.inline.<base64url(Payload JSON)>.<base64url(HMAC-SHA256)>
```

remote token 的第四段解码后直接是 `RemoteSpec`：

```text
dp.21.remote.<base64url(RemoteSpec JSON)>.<base64url(HMAC-SHA256)>
```

HMAC 输入是 token 前四段的原文拼接：`dp.21.<kind>.<base64url-json>`；服务端使用 `server.proxy.directive.token-secret` 计算并以 constant-time compare 校验。secret 不进入 token。

统一处理流程：

```text
inline token -> Payload -------------------------┐
                                                 ├─> DecodePayload -> Validate -> CompilePayload
remote token -> RemoteSpec -> typed reference -> HTTP/Redis/File reader ─┘
```

RemoteSpec 只允许描述如何读取 Payload：

```json
{
  "http": {
    "url": "https://resolver.example.com/v1/team-a/service-a",
    "headers": {
      "mode": "patch",
      "mutations": [
        {"side": "request", "action": "set", "name": "Authorization", "values": ["Bearer resolver-token"]}
      ]
    }
  }
}
```

RemoteSpec 是严格 backend one-of，顶层必须且只能包含 `http`、`redis` 或 `file` 之一，不使用共享 `type` 字段。Redis 使用标准连接 URL 与独立 key：

```json
{
  "redis": {
    "url": "redis://user:password@redis.example.com:6379/1",
    "key": "team-a/service-a"
  }
}
```

File 只声明配置根目录内的文件路径：

```json
{"file":{"path":"team-a/services/primary.json"}}
```

`path` 使用 slash 分隔并支持子目录，必须是非空相对路径；不接受绝对路径、`.`、`..`、反斜杠或超过 4096 字节的值。每个 backend 子对象严格拒绝其他 backend 的字段。

RemoteSpec 不使用 `source` 包裹，也不能声明以下字段：

- `payload`；
- `modules`；
- `recovery`；
- target、proxy、业务 headers 等任何执行字段。

这些字段出现时必须作为未知字段拒绝，不做兼容或隐式迁移。

## Payload 是唯一执行模型

inline token 内容、HTTP response body、Redis JSON 根文档和 File 文件内容使用完全相同的 `Payload` schema。Payload 可以包含：

- 可选的 string metadata；core 预设 `user_id`、`user_key` 常用 key，但不要求业务身份字段，`trace_id` 由 Exchange 保留并注入；
- 严格 one-of target：`base_url` 或 `exact_url`；
- SOCKS5 proxy；
- request/response header policy；
- 单一有序 `modules` 数组，每项和 `recovery.controller` 都直接使用统一的 `{module, config}` Module Spec；生命周期和 capability 由全局 Catalog 中的 Definition 声明；
- Recovery Controller、triggers 与 budget。

Payload 的 `headers` 是一个统一 HeaderPolicy，不再拆分 `request` / `response` 子对象：

```json
{
  "headers": {
    "mode": "patch",
    "preserve_proxy_disclosure": false,
    "mutations": [
      {"side": "request", "action": "set", "name": "Authorization", "values": ["Bearer upstream-token"]},
      {"side": "response", "action": "remove", "name": "Server"}
    ]
  }
}
```

`side` 是每条 mutation 的必填字段，只允许 `request` 或 `response`；`action` 只允许 `set`、`remove` 或 `append`。mutations 按数组顺序执行。`mode` 与 `preserve_proxy_disclosure` 只影响 request 基线；response 没有独立 mode。

remote resolver 返回示例：

```json
{
  "metadata": {"user_id": "user-1", "user_key": "key-1"},
  "target": {"base_url": "https://api.example.com/v1"},
  "modules": [
    {"module": "builtin.capture", "config": {}},
    {"module": "builtin.llmusage", "config": {"protocol": "openai.responses"}}
  ],
  "recovery": {
    "controller": {"module": "builtin.recovery", "config": {"url": "https://controller.example.com/recovery"}},
    "triggers": {"transport_error": true},
    "budget": {"max_round_trips": 3}
  }
}
```

不存在 token envelope 与远端 Payload 的字段合并，也不存在优先级规则。Remote 解引用成功后，后续编译和执行不再区分 Payload 来自 inline、HTTP、Redis 还是 File。

`base_url` 在 Prepare 阶段与入站 path/query 合成为最终 URL；`exact_url` 原样作为最终 URL并忽略入站 path/query。两者必须且只能出现一个。proxy Plan 只保存编译后的最终 URL，不携带 target 合成策略。

## 读取生命周期

RemoteSpec 在 `Prepare` 阶段解引用一次。得到的 Payload 会被严格解码、规范化并编译为当前请求唯一的 `PreparedDirective`，固定包含 Source、HTTP Plan、Program、Recovery 和 Metadata；各 RoundTrip 消费相同的编译结果。

这意味着：

- exchange-lifetime Module 能在读取请求正文前正确初始化；
- Recovery policy 在第一个 RoundTrip 前已经确定；
- 同一请求不会因远端内容在 RoundTrip 之间变化而产生隐式配置漂移；
- Recovery retry 重放相同的已解析 Payload，不会重新读取远端值；
- 不提供旧 plan 回退、字段 merge、递归 remote token 或跨请求 value cache。

如果业务需要下一次请求使用新配置，应先更新远端 Payload，再发起新的代理请求。

## 适配器边界

core 将 RemoteSpec 编译成只包含合法运行时状态的 typed reference，并通过三个精确端口读取远端内容：

```go
type HTTPRemoteReader interface {
    Read(context.Context, HTTPReference, RequestSnapshot) ([]byte, error)
}

type RedisRemoteReader interface {
    Read(context.Context, RedisReference) ([]byte, error)
}

type FileRemoteReader interface {
    Read(context.Context, FileReference) ([]byte, error)
}
```

HTTP reader 可以使用清理和改写后的直接请求头以及有限请求元数据生成 Payload；Redis reader 只按 URL 和 key 读取 JSON；File reader 只按配置 root 和相对 path 读取文件。adapter 只返回原始 JSON 字节，不应：

- 理解或修改 Payload 字段；
- 组装 `proxy.Plan`；
- 增加存储专属 envelope；
- 返回另一条 remote token；
- 提供缺省 Payload 或读取失败回退；
- 缓存 directive value。

core resolver 根据 RemoteSpec 唯一非空的 backend 分支调用对应端口。组合根 `internal/appcmd/server` 只注入 HTTP/Redis/File adapter 并管理资源，不参与每次请求的类型分派。core 不依赖具体 adapter。

通用 header plan 和执行语义位于 `internal/core/httpheader`；directive core 负责编译 HeaderPolicy，proxy 与 HTTP remote adapter 负责在各自请求上应用同一种 plan。

## HTTP 适配器

HTTP adapter 向 `RemoteSpec.http.url` 发起请求，并把成功 response body 原样交给 directive core。URL path/query 完整标识 resolver 资源，不存在独立 HTTP key。

- URL 只允许 HTTP/HTTPS，不能包含 userinfo 或 fragment；
- HTTPS resolver 显式启用 HTTP/2 并通过 ALPN 优先协商 `h2`，服务端不支持时回退 HTTP/1.1；明文 HTTP 不隐式启用 h2c；
- resolver 共用长生命周期 `http.Client`，并与上游读取同一份 `server.proxy.transport` 连接池配置，但使用独立的 transport 实例；
- `headers` 直接复用 Payload HeaderPolicy 的 `mode`、`preserve_proxy_disclosure` 与 `mutations`；
- HTTP RemoteSpec 的 mutation 只允许 `side: request`，因为该策略只改写 resolver 请求头；
- patch 模式以原入站 header 为基线，replace 模式从空集合开始；
- directive header policy 优先于协议默认 header，可以覆盖或移除 `Content-Type`；
- directive Authorization 和原 Content-Length 在 mutations 前移除，mutations 可重新设置 resolver Authorization；
- 代理披露 header 默认在 mutations 前移除；`x-dp-*` 和 hop-by-hop header 在 mutations 后统一移除；
- resolver metadata JSON 只包含 protocol 以及原请求的 method、URL 和 host，不包含 key，也不复制 headers；
- `204/404` 表示 not found；其他非成功响应表示 unavailable 或 invalid。

## Redis 适配器

Redis 运行前提是 Redis 8+。每条 Payload 使用 Redis JSON 保存，并从根文档读取：

```shell
redis-cli JSON.SET 'dp:directive:team-a/service-a' '$' \
  '{"metadata":{"user_key":"key-1"},"target":{"base_url":"https://api.example.com/v1"}}'

redis-cli JSON.GET 'dp:directive:team-a/service-a'
```

Redis adapter：

- 只执行 `JSON.GET key`；
- 不兼容普通 String key；
- 不添加 key prefix；
- 不解释 JSONPath；
- 可以缓存动态 Redis client，但不能缓存 Payload value。

合法 JSON 不等于合法 Payload。缺少 target one-of、同时声明 `base_url`/`exact_url`、包含未知字段或字段组合错误仍由统一 `DecodePayload` 拒绝。

## File 适配器

File adapter 的根目录由 `server.proxy.directive.remote.file.root` 配置，RemoteSpec 的 `file` 分支只携带相对 `path`。adapter：

- 使用 `os.OpenRoot` 在配置根目录内打开文件，允许访问任意层级子目录；
- 拒绝越过根目录的路径和指向根目录外部的符号链接；
- 只读取普通文件，不读取目录、设备或其他特殊文件；
- 每次请求直接读取当前文件内容，不缓存 Payload value；
- 与 HTTP/Redis 共用 `max-payload-bytes` 上限。

## 错误语义

| 场景 | 语义 |
| --- | --- |
| HTTP `204/404`、Redis key 或 File 文件不存在 | remote directive not found |
| 连接失败、超时、认证失败或 File root 不可用 | remote directive unavailable |
| 响应、文档或文件超过大小限制 | remote directive invalid |
| Redis key 类型不是 JSON | remote directive invalid |
| File path 不是普通文件 | remote directive invalid |
| Payload JSON/schema 不合法 | remote directive invalid |

RemoteSpec、完整 endpoint、URL query、Redis URL/key、File path/root、认证信息和底层 adapter 错误属于正常观测信息，日志和事件不对这些字段做脱敏。来源事件使用 `endpoint` 表示 HTTP/Redis URL，使用 `resource` 表示 Redis key 或 File path。是否记录 token 原文或远端 Payload 内容由具体事件协议决定。

## 变更审查清单

- inline body 是否仍直接等于 Payload？
- remote body 是否仍直接等于 RemoteSpec？
- RemoteSpec 是否只包含读取信息？
- RemoteSpec 是否严格且只包含一个 `http`、`redis` 或 `file` 分支？
- HTTP、Redis 与 File 是否返回完全相同的 Payload schema？
- Header mutation 是否始终显式声明 side 并使用 `set|remove|append`，且 HTTP RemoteSpec 是否只包含 request mutation？
- modules 和 recovery 是否只存在于 Payload？
- 是否意外引入了字段 merge、递归 remote、默认回退或 value cache？
- Remote 是否仍在 Prepare 阶段只解引用一次？
- Redis 是否仍只读取 JSON 根文档并拒绝 String key？
- File 是否仍受配置 root 约束、支持子目录且只读取普通文件？
