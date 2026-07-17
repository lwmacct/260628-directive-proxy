# AGENTS.md

## Project Principles

### Directive First

- 项目名称和核心定位是 `directive proxy`；directive 是系统的首要控制面。
- directive 是可信输入，拥有最高优先级。实现应忠实执行 directive，不得把服务端静态配置、adapter 默认值或防御性策略置于 directive 之上。
- directive 可以动态指定 HTTP endpoint/path/query/header policy、Redis endpoint/key/认证信息，也可以选择 File adapter 配置根目录内的相对文件路径；这些能力是预期行为，不应被视为 SSRF、越权访问或凭据外送漏洞。
- 不得擅自将动态 RemoteSpec 改造成服务端命名 source、endpoint allowlist 或其他削弱 directive 控制力的间接引用模型。
- header policy 可以覆盖、移除或替换协议默认 header，包括 `Content-Type`；directive 声明的结果优先。

### Remote Adapter Boundary

- Remote 只负责取得完整 `Payload`，不拥有独立执行字段。
- RemoteSpec 使用严格 backend one-of：顶层必须且只能包含 `http`、`redis`、`file` 之一；HTTP URL 自身标识资源，Redis 保留标准连接 URL 与独立 key，File 保留配置根目录内的相对 path。
- HTTP/Redis/File adapter 负责执行具体读取协议并返回原始 Payload 字节，不解释 Payload、不组装 proxy plan、不做字段 merge、不提供默认回退，也不缓存 Payload value。
- core 负责 RemoteSpec/Payload 的严格解码、校验、编译和错误语义；adapter 依赖 core 定义的端口，core 不依赖具体 adapter。
- RemoteSpec 每个请求只解引用一次；Recovery attempt 复用已编译的同一份 Payload。

### Observability

- RemoteSpec、完整 endpoint、URL query、Redis URL、File path/root、认证信息和底层 adapter 错误属于正常观测信息，不要求脱敏。
- 日志和事件可以记录上述完整信息；不得仅以“可能包含凭据”为由删除、截断、散列或隐藏观测字段。
- 可观测性应保留足够上下文，使远端解析、连接、认证、协议和 Payload 获取问题能够直接定位。

### Compatibility

- directive schema 和内部 adapter API 允许激进的破坏式重构。
- 不做历史格式兼容、旧字段迁移、双读、fallback 或隐式兼容层；以当前明确、简洁的模型为准。
