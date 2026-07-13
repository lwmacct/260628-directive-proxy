# Directive 远程适配器设计约束

状态：已采纳

本文记录 directive 远程读取的设计前提、稳定边界和非目标。后续修改 HTTP、Redis 或新增远程来源时，应以本文作为架构判断依据，避免把可信的自包含指令误改造成服务端注册中心、身份系统或状态管理系统。

## 设计背景

directive token 由可信人员人工编写和管理，不是由不可信客户端任意生成，也不是机器在每次请求时实时签发。因此，这里的核心问题是如何准确、简单地表达一条代理指令，而不是如何为 token 增加身份认证、签名、授权、随机引用或防枚举能力。

remote directive 的目的，是在 token 不适合直接容纳完整 payload 时，从外部位置取得同一份完整 directive。HTTP 和 Redis 都只是读取外部 JSON 的适配器，不拥有独立的 directive 语义。

## 核心原则

### Token 是可信且自包含的配置

remote token 中的 `RemoteSpec` 应完整描述如何取得远端内容，包括：

- 适配器类型；
- HTTP 或 Redis URL；
- 远端 key；
- HTTP resolver 所需的静态 header；
- 允许传递给 HTTP resolver 的请求 header selector。

连接地址和凭据可以出现在可信 token 中。服务端不应要求预先注册后端别名，也不应把 URL 替换成服务端维护的 opaque ID。

这里的自包含性是有意设计，具有以下价值：

- token 可以独立复制、保存和使用；
- 同一个服务实例可以访问多个动态后端；
- 不引入 token 与服务端配置之间的隐式映射；
- HTTP 与 Redis 保持一致的远端描述模型。

### 适配器只负责读取

所有远端适配器遵循相同边界，由组合根提供统一分派：

```go
type RemoteReader interface {
	Read(context.Context, RemoteSpec, *http.Request) ([]byte, error)
}
```

适配器的输出是完整 directive JSON 的原始字节。适配器不应：

- 把远端内容解释成另一套业务结构；
- 在适配器内部组装或编译 `proxy.Plan`；
- 合并 token 与远端 payload；
- 提供缺省 payload 或读取失败后的回退；
- 递归解析另一条 remote directive；
- 缓存 directive value。

远端内容取得后，必须与 inline payload 共用唯一的严格解码、校验和编译流程：

```text
inline token payload -------------------------┐
                                              ├─> DecodePayload -> ToPlan
RemoteSpec -> HTTP adapter -> response body --┤
RemoteSpec -> Redis adapter -> JSON document -┘
```

因此，来源不同不能导致 payload schema、字段默认值或校验规则不同。

具体 HTTP 与 Redis adapter 位于相互独立的包中，不彼此导入。`internal/appcmd/server` 作为组合根创建 adapter、按 `RemoteSpec.type` 分派，并统一关闭资源。core 只依赖 `RemoteReader` 接口，不依赖具体 adapter。

### 远端内容就是裸 Payload

HTTP response body 和 Redis JSON 根文档都必须直接保存完整 `Payload`：

```json
{
  "target": {
    "url": "https://api.example.com/v1"
  },
  "headers": {
    "mode": "patch",
    "ops": []
  }
}
```

不得为了 Redis 增加 `kind`、`metadata`、`revision`、`enabled`、`spec` 等专属包络。协议版本属于 directive token 协议和应用解析规则，不由某个存储适配器另行定义。

如果未来确实需要修改 payload schema，应统一修改 inline、HTTP 和 Redis 三种来源的解码规则，而不是为某个来源增加私有格式。

## HTTP 适配器

HTTP adapter 向 `RemoteSpec.url` 发起请求，并把成功响应的 body 原样交给 core directive 层。

HTTP resolver 可以根据有限的请求元数据动态选择或生成 directive，这是 HTTP 适配器相对于静态存储的能力。允许披露的请求 header 必须继续由 `RemoteSpec.request_headers` 显式选择，directive `Authorization` 和 hop-by-hop header 不得发送。

HTTP adapter 不应自行理解返回的 JSON 字段。HTTP 返回的仍然是完整裸 `Payload`，而不是引用、补丁或 adapter-specific envelope。

## Redis 适配器

### 数据模型

Redis 运行前提是 Redis 8+。每条 directive 使用 Redis JSON 类型保存，key 仍是 `RemoteSpec.key` 指定的普通 Redis key：

```shell
redis-cli JSON.SET 'dproxy:directive:team-a/openai' '$' \
  '{"target":{"url":"https://api.example.com/v1"}}'
```

读取必须针对根文档：

```shell
redis-cli JSON.GET 'dproxy:directive:team-a/openai'
```

Redis adapter 不添加 key prefix，不转换 key，也不解释 JSONPath。key 的命名空间由可信 token 编写者和 Redis 数据管理方共同约定。

### 为什么使用 Redis JSON

使用 Redis JSON 的目的限定为存储层收益：

- 写入时保证内容是合法 JSON；
- Redis 类型能表达该 key 保存的是结构化文档；
- 运维工具可以观察或管理 JSON 文档；
- 避免普通 String 混入任意文本。

Redis JSON 不能替代应用层 schema 校验。合法 JSON 仍可能不是合法 directive，例如根节点为数组、缺少 `target.url`、包含未知字段或字段组合不合法。这些情况必须由统一的 `DecodePayload` 和 `ToPlan` 拒绝。

### 不兼容旧 String key

Redis adapter 只执行 `JSON.GET key`，不再执行普通 `GET`，也不在失败后回退到 `GET`。旧 String key 必须由数据管理方显式迁移为 Redis JSON 文档。

禁止双读的原因是：

- 系统只保留一个明确的数据类型契约；
- 配置错误可以立即暴露；
- 不让历史格式永久进入读取路径；
- 不增加不同 Redis 类型之间的隐式行为差异。

### 连接管理

由于 `RemoteSpec` 可以包含不同 Redis URL，动态 Redis client 及其有界缓存是该适配器的合理实现细节。缓存的是连接 client，而不是 directive value。

连接缓存可以限制容量、空闲时间和单 client 连接池，但不得改变自包含 `RemoteSpec` 的语义，也不得把动态 URL 收敛为服务端注册的后端名称。

## 错误语义

适配器应尽可能区分数据错误和基础设施错误：

| 场景 | 语义 |
| --- | --- |
| HTTP `204/404` 或 Redis key 不存在 | remote directive not found |
| 连接失败、超时、认证失败 | remote directive unavailable |
| 响应或文档超过大小限制 | remote directive invalid |
| Redis key 类型不是 JSON | remote directive invalid |
| JSON 根节点或 payload schema 不合法 | remote directive invalid |
| Redis 不支持所需的 `JSON.GET` 命令 | remote directive unavailable / deployment misconfiguration |

最终对外错误由 core directive 层统一映射。适配器错误不得绕过统一解析流程，也不应把远端原文或连接凭据写入日志。

## 明确拒绝的方向

除非设计前提发生变化并先更新本文，否则不采用以下方案：

- 不把 Redis 改造成中心 directive registry；
- 不把 remote token 改成随机 opaque ID；
- 不要求服务端预注册 Redis/HTTP 后端别名；
- 不因为 URL 位于 token 内而默认其不可信；
- 不为人工 token 增加签名、revision、过期时间或核对信息；
- 不为 Redis payload 增加存储专属 envelope；
- 不让 Redis adapter 直接返回 `proxy.Plan`；
- 不允许 HTTP 和 Redis 使用不同的 payload schema；
- 不对 Redis String 做历史兼容或静默迁移；
- 不缓存远端 directive value。

这些方案并非在所有系统中都错误，而是不符合本项目“可信人工 token、自包含 RemoteSpec、远端来源仅为读取适配器”的既定条件。

## 允许的演进

以下变化符合当前设计：

- 新增 S3、Consul 等只读 adapter，只要仍返回完整裸 `Payload`；
- 改善连接 client 的复用、容量限制和生命周期管理；
- 完善不同后端的错误分类；
- 增加真实 Redis 8 集成测试；
- 统一演进所有来源共享的 directive payload schema；
- 加强凭据日志脱敏和响应大小限制。

只有当 adapter 数量明显增加且组合根中的显式分派变得难以维护时，才考虑 adapter registry。注册式分派是实现选择，不是当前架构目标。

## 变更审查清单

修改 directive remote 相关代码时，应逐项确认：

- inline、HTTP 和 Redis 是否仍使用同一 `Payload` schema？
- adapter 是否仍只负责取得原始完整 JSON？
- `RemoteSpec` 是否仍然自包含？
- 是否意外引入了服务端后端注册或 token 状态？
- 是否对远端 payload 做了合并、回退、递归引用或 value 缓存？
- Redis 是否仍只读取 JSON 根文档，并拒绝 String key？
- 数据无效与后端不可用是否得到合理区分？
- URL、凭据和远端原文是否避免进入日志？
- 新增来源是否保持与既有来源相同的解析和校验语义？

任何一项答案不明确时，应先重新核对本文的设计前提，再决定是否修改架构。
