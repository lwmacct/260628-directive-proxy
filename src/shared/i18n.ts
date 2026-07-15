import { useWorkbenchLocale } from "@lwmacct/260627-antd-workbench";

const zh = {
  app: {
    console: "控制台", settings: "设置", debugTools: "调试工具", preferences: "偏好设置",
    exchanges: "活动请求", authConsole: "Authorization 工作台", appearance: "外观设置",
  },
  auth: {
    authorizedOnly: "仅限已授权用户访问", signInDescription: "登录以继续",
    unavailable: "暂时无法连接认证服务",
  invalidToken: "访问令牌无效",
  },
  appearance: { panel: "主题与界面" },
  exchanges: {
    description: "查看处于正文内存排队、正文读取、远端指令解析或等待上游最终响应头阶段的请求，并人工触发带前置条件的重试。",
    auto: "自动刷新", refresh: "刷新", active: "活动请求", retryable: "可重试", oldestWait: "最长阶段时长",
    time: "尝试时间", method: "方法", status: "状态", metadata: "请求元数据", search: "搜索 URL、目标地址、Metadata 或 Trace ID",
    target: "目标地址", attempt: "尝试", waiting: "阶段时长", resolving: "解析远程指令", memoryWait: "等待正文内存", bodyReading: "读取请求正文", awaiting: "等待上游响应", retrying: "正在切换尝试",
    retry: "重试", retryConfirm: "取消当前上游尝试并启动下一次尝试？", retryNotReady: "当前请求不可重试或已达到次数上限",
    requestFailed: "请求失败", retryFailed: "重试失败",
  },
  authConsole: {
    description: "从结构化表单、Payload JSON 或 Token 任一来源编辑 directive，并同步生成其他格式。",
    structured: "结构化编辑", editableSources: "可编辑输入源", directiveSource: "指令来源", redisKey: "Redis Key", optionalRemoteKey: "远程 Key（可选）", httpResolverURL: "HTTP Resolver URL", redisURL: "Redis URL", resolverHeaders: "Resolver 静态请求头", resolverRequestHeaders: "允许披露的原请求头", addResolverHeader: "添加请求头", removeResolverHeader: "删除 Resolver 请求头", enabled: "启用", headerMode: "Header Mode", headerOps: "Header 操作", requestHeaderPolicy: "请求 Header", responseHeaderPolicy: "响应 Header", preserveProxyDisclosure: "保留代理披露 Header",
    add: "添加", op: "操作", match: "匹配", selector: "选择器", values: "值", set: "设置", remove: "移除",
    exact: "精确", valuePlaceholder: "输入值后按 Enter", removeHeaderOp: "删除 Header 操作",
    dirty: "有未应用修改", synced: "已同步", copyPayload: "复制 Payload", copyToken: "复制 Token",
    applyPayload: "应用 Payload", parseToken: "解析 Token", payloadApplied: "Payload 已应用到表单和 Token",
    payloadParseFailed: "Payload JSON 解析失败", tokenApplied: "Token 已解析并应用到表单和 Payload",
    tokenParseFailed: "Token 解析失败", requestDebug: "请求调试", requestDescription: "请求发送到当前站点的 data plane，并自动使用工作台当前生成的 Token。",
    cancel: "取消", send: "发起请求", bodyDisabled: (method: string) => `${method} 请求不会发送 Body`,
    waiting: "等待发起请求", requestCancelled: "请求已取消", requestFailed: "请求失败",
    copied: "已复制", copyFailed: "复制失败", directiveNotReady: "Directive 尚未通过后端校验",
    requestHeaders: "请求头 JSON", requestBody: "请求正文", responseHeaders: "响应头", responseBody: "响应正文",
    invalidJSON: (label: string) => `${label} 不是有效的 JSON`, mustBe: (label: string, type: string) => `${label} 必须是 ${type}`,
    nonEmptyString: (label: string) => `${label} 必须是非空字符串`, onlyValues: (label: string, values: string) => `${label} 只能是 ${values}`,
    exactlyOneSelector: (label: string) => `${label} 必须且只能包含 name 或 glob 之一`, invalidHeaderName: (label: string) => `${label} 不是合法的 Header 名`,
    invalidGlob: (label: string) => `${label} 不是合法的 Glob`, removeHasValues: (label: string) => `${label} Remove 操作不能包含 values`,
    setNeedsValues: (label: string) => `${label} Set/Add 操作必须包含 values`, hostValues: (label: string) => `${label} Host 只支持单值 Set 或 Remove`,
    tokenPrefix: "Token 必须使用当前 dproxy i/r 格式", tokenPayloadMissing: "Token 缺少 payload", tokenDecodeFailed: "Token payload 解码失败", invalidRedisKey: "远程 Key 必须是 1-256 字节且不能包含首尾空白或控制字符", invalidRemoteURL: "远程 URL 与所选类型不匹配", invalidResolverHeader: "Resolver 请求头名称或值不合法",
    unknownField: (label: string, field: string) => `${label} 包含未知字段 ${field}`, pathRequired: "请求路径不能为空",
    pathSameOrigin: "请求路径必须是以 / 开头的同源路径", pathReservedAPI: "请求路径不能指向保留 API",
    headerValueString: (name: string) => `Request Header ${name} 的值必须是 string`,
  },
} as const;

const en: Text = {
  app: {
    console: "Console", settings: "Settings", debugTools: "Debug tools", preferences: "Preferences",
    exchanges: "Active requests", authConsole: "Authorization Workbench", appearance: "Appearance",
  },
  auth: {
    authorizedOnly: "Authorized users only", signInDescription: "Sign in to continue",
    unavailable: "Unable to connect to the authentication service",
  invalidToken: "Invalid access token",
  },
  appearance: { panel: "Theme and interface" },
  exchanges: {
    description: "View requests waiting for body memory, reading a body, resolving a directive, or awaiting final upstream response headers, and trigger a conditional retry through the admin API.",
    auto: "Auto refresh", refresh: "Refresh", active: "Active requests", retryable: "Retryable", oldestWait: "Oldest state",
    time: "Attempt time", method: "Method", status: "Status", metadata: "Request metadata", search: "Search URL, target, metadata, or Trace ID",
    target: "Target", attempt: "Attempt", waiting: "State time", resolving: "Resolving directive", memoryWait: "Waiting for body memory", bodyReading: "Reading request body", awaiting: "Awaiting upstream", retrying: "Switching attempt",
    retry: "Retry", retryConfirm: "Cancel the current upstream attempt and start the next attempt?", retryNotReady: "Request is not retryable or attempt limit reached",
    requestFailed: "Request failed", retryFailed: "Retry failed",
  },
  authConsole: {
    description: "Edit a directive from the structured form, Payload JSON, or Token and keep every format in sync.",
    structured: "Structured editor", editableSources: "Editable sources", directiveSource: "Directive source", redisKey: "Redis key", optionalRemoteKey: "Remote key (optional)", httpResolverURL: "HTTP resolver URL", redisURL: "Redis URL", resolverHeaders: "Static resolver headers", resolverRequestHeaders: "Disclosed request headers", addResolverHeader: "Add header", removeResolverHeader: "Remove resolver header", enabled: "Enabled", headerMode: "Header Mode", headerOps: "Header operations", requestHeaderPolicy: "Request headers", responseHeaderPolicy: "Response headers", preserveProxyDisclosure: "Preserve proxy disclosure headers",
    add: "Add", op: "Op", match: "Match", selector: "Selector", values: "Values", set: "Set", remove: "Remove",
    exact: "Exact", valuePlaceholder: "Type a value and press Enter", removeHeaderOp: "Remove header operation",
    dirty: "Unapplied changes", synced: "Synced", copyPayload: "Copy Payload", copyToken: "Copy Token",
    applyPayload: "Apply Payload", parseToken: "Parse Token", payloadApplied: "Payload applied to the form and Token",
    payloadParseFailed: "Failed to parse Payload JSON", tokenApplied: "Token parsed and applied to the form and Payload",
    tokenParseFailed: "Failed to parse Token", requestDebug: "Request debugger", requestDescription: "Send a request to this site's data plane using the Token currently generated by the workbench.",
    cancel: "Cancel", send: "Send request", bodyDisabled: (method: string) => `${method} requests do not send a body`,
    waiting: "Waiting to send a request", requestCancelled: "Request cancelled", requestFailed: "Request failed",
    copied: "Copied", copyFailed: "Copy failed", directiveNotReady: "Directive has not passed backend validation",
    requestHeaders: "Request headers JSON", requestBody: "Request body", responseHeaders: "Response headers", responseBody: "Response body",
    invalidJSON: (label: string) => `${label} is not valid JSON`, mustBe: (label: string, type: string) => `${label} must be ${type}`,
    nonEmptyString: (label: string) => `${label} must be a non-empty string`, onlyValues: (label: string, values: string) => `${label} must be one of ${values}`,
    exactlyOneSelector: (label: string) => `${label} must contain exactly one of name or glob`, invalidHeaderName: (label: string) => `${label} is not a valid header name`,
    invalidGlob: (label: string) => `${label} is not a valid glob`, removeHasValues: (label: string) => `${label} cannot contain values for Remove`,
    setNeedsValues: (label: string) => `${label} requires values for Set/Add`, hostValues: (label: string) => `${label} Host only supports single-value Set or Remove`,
    tokenPrefix: "Token must use the current dproxy i/r format", tokenPayloadMissing: "Token payload is missing", tokenDecodeFailed: "Failed to decode Token payload", invalidRedisKey: "Remote key must be 1-256 bytes without surrounding whitespace or control characters", invalidRemoteURL: "Remote URL does not match the selected type", invalidResolverHeader: "Resolver header name or value is invalid",
    unknownField: (label: string, field: string) => `${label} contains unknown field ${field}`, pathRequired: "Request path is required",
    pathSameOrigin: "Request path must be a same-origin path starting with /", pathReservedAPI: "Request path cannot target a reserved API",
    headerValueString: (name: string) => `Request Header ${name} must be a string`,
  },
};

type Widen<T> = { [K in keyof T]: T[K] extends (...args: infer A) => unknown ? (...args: A) => string : T[K] extends object ? Widen<T[K]> : string };
export type Text = Widen<typeof zh>;

export function useText(): Text {
  const { locale } = useWorkbenchLocale();
  return locale === "en-US" ? en : zh;
}
