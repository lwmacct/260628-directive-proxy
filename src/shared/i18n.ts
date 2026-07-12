import { useWorkbenchLocale } from "@lwmacct/260627-antd-workbench";

const zh = {
  app: {
    console: "控制台", settings: "设置", debugTools: "调试工具", preferences: "偏好设置",
    exchanges: "请求记录", authConsole: "Authorization 工作台", appearance: "外观设置",
  },
  auth: {
    authorizedOnly: "仅限已授权用户访问", signInDescription: "登录以继续",
    unavailable: "暂时无法连接认证服务",
  },
  appearance: { panel: "主题与界面" },
  exchanges: {
    description: "查看、过滤和管理最近的代理请求响应记录。", capture: "捕获", auto: "自动刷新",
    refresh: "刷新", retained: "保留数量", capacity: "容量", total: "累计数量", bodyLimit: "正文上限",
    time: "时间", method: "方法", status: "状态", latency: "耗时", body: "正文", details: "详情",
    search: "搜索 URL、目标地址或 ID", errors: "仅错误", apply: "应用", clear: "清空",
    clearConfirm: "清空所有保留的请求记录？", requestFailed: "请求失败", updateFailed: "更新失败",
    clearFailed: "清空失败", open: "处理中", exchange: "请求", started: "开始时间", duration: "持续时间",
    target: "目标地址", directiveSource: "指令来源", directiveKey: "Redis Key", directiveLookup: "Redis 查询耗时", requestBody: "请求正文", responseBody: "响应正文", requestHeaders: "入站请求头", outboundRequestHeaders: "出站请求头",
    responseHeaders: "响应头", captured: "已捕获",
  },
  authConsole: {
    description: "从结构化表单、Payload JSON 或 Token 任一来源编辑 directive，并同步生成其他格式。",
    structured: "结构化编辑", editableSources: "可编辑输入源", directiveSource: "指令来源", redisKey: "Redis Key", enabled: "启用", headerOps: "Header 操作",
    add: "添加", op: "操作", match: "匹配", selector: "选择器", values: "值", set: "设置", remove: "移除",
    exact: "精确", preset: "预设", valuePlaceholder: "输入值后按 Enter", removeHeaderOp: "删除 Header 操作",
    dirty: "有未应用修改", synced: "已同步", copyPayload: "复制 Payload", copyToken: "复制 Token",
    applyPayload: "应用 Payload", parseToken: "解析 Token", payloadApplied: "Payload 已应用到表单和 Token",
    payloadParseFailed: "Payload JSON 解析失败", tokenApplied: "Token 已解析并应用到表单和 Payload",
    tokenParseFailed: "Token 解析失败", requestDebug: "请求调试", requestDescription: "请求发送到当前站点的 data plane，并自动使用工作台当前生成的 Token。",
    cancel: "取消", send: "发起请求", bodyDisabled: (method: string) => `${method} 请求不会发送 Body`,
    waiting: "等待发起请求", requestCancelled: "请求已取消", requestFailed: "请求失败",
    copied: "已复制", copyFailed: "复制失败",
    requestHeaders: "请求头 JSON", requestBody: "请求正文", responseHeaders: "响应头", responseBody: "响应正文",
    invalidJSON: (label: string) => `${label} 不是有效的 JSON`, mustBe: (label: string, type: string) => `${label} 必须是 ${type}`,
    nonEmptyString: (label: string) => `${label} 必须是非空字符串`, onlyValues: (label: string, values: string) => `${label} 只能是 ${values}`,
    exactlyOneSelector: (label: string) => `${label} 必须且只能包含 name、glob 或 preset 之一`, invalidHeaderName: (label: string) => `${label} 不是合法的 Header 名`,
    invalidGlob: (label: string) => `${label} 不是合法的 Glob`, removeHasValues: (label: string) => `${label} Remove 操作不能包含 values`, presetOnlyRemove: (label: string) => `${label} preset 只支持 Remove`,
    setNeedsValues: (label: string) => `${label} Set/Add 操作必须包含 values`, hostValues: (label: string) => `${label} Host 只支持单值 Set 或 Remove`,
    tokenPrefix: "Token 必须是 dproxy.12.i 或 dproxy.12.r 格式", tokenPayloadMissing: "Token 缺少 payload", tokenDecodeFailed: "Token payload 解码失败", invalidRedisKey: "Redis Key 必须是 1-256 字节且不能包含首尾空白或控制字符",
    unknownField: (label: string, field: string) => `${label} 包含未知字段 ${field}`, pathRequired: "请求路径不能为空",
    pathSameOrigin: "请求路径必须是以 / 开头的同源路径", pathControlPlane: "请求路径不能指向 control plane",
    headerValueString: (name: string) => `Request Header ${name} 的值必须是 string`,
  },
} as const;

const en: Text = {
  app: {
    console: "Console", settings: "Settings", debugTools: "Debug tools", preferences: "Preferences",
    exchanges: "Exchanges", authConsole: "Authorization Workbench", appearance: "Appearance",
  },
  auth: {
    authorizedOnly: "Authorized users only", signInDescription: "Sign in to continue",
    unavailable: "Unable to connect to the authentication service",
  },
  appearance: { panel: "Theme and interface" },
  exchanges: {
    description: "View, filter, and manage recent proxy request and response records.", capture: "Capture", auto: "Auto refresh",
    refresh: "Refresh", retained: "Retained", capacity: "Capacity", total: "Total", bodyLimit: "Body limit",
    time: "Time", method: "Method", status: "Status", latency: "Latency", body: "Body", details: "Details",
    search: "Search URL, target, or ID", errors: "Errors only", apply: "Apply", clear: "Clear",
    clearConfirm: "Clear all retained exchange records?", requestFailed: "Request failed", updateFailed: "Update failed",
    clearFailed: "Clear failed", open: "open", exchange: "Exchange", started: "Started", duration: "Duration",
    target: "Target", directiveSource: "Directive source", directiveKey: "Redis key", directiveLookup: "Redis lookup", requestBody: "Request body", responseBody: "Response body", requestHeaders: "Inbound request headers", outboundRequestHeaders: "Outbound request headers",
    responseHeaders: "Response headers", captured: "captured",
  },
  authConsole: {
    description: "Edit a directive from the structured form, Payload JSON, or Token and keep every format in sync.",
    structured: "Structured editor", editableSources: "Editable sources", directiveSource: "Directive source", redisKey: "Redis key", enabled: "Enabled", headerOps: "Header operations",
    add: "Add", op: "Op", match: "Match", selector: "Selector", values: "Values", set: "Set", remove: "Remove",
    exact: "Exact", preset: "Preset", valuePlaceholder: "Type a value and press Enter", removeHeaderOp: "Remove header operation",
    dirty: "Unapplied changes", synced: "Synced", copyPayload: "Copy Payload", copyToken: "Copy Token",
    applyPayload: "Apply Payload", parseToken: "Parse Token", payloadApplied: "Payload applied to the form and Token",
    payloadParseFailed: "Failed to parse Payload JSON", tokenApplied: "Token parsed and applied to the form and Payload",
    tokenParseFailed: "Failed to parse Token", requestDebug: "Request debugger", requestDescription: "Send a request to this site's data plane using the Token currently generated by the workbench.",
    cancel: "Cancel", send: "Send request", bodyDisabled: (method: string) => `${method} requests do not send a body`,
    waiting: "Waiting to send a request", requestCancelled: "Request cancelled", requestFailed: "Request failed",
    copied: "Copied", copyFailed: "Copy failed",
    requestHeaders: "Request headers JSON", requestBody: "Request body", responseHeaders: "Response headers", responseBody: "Response body",
    invalidJSON: (label: string) => `${label} is not valid JSON`, mustBe: (label: string, type: string) => `${label} must be ${type}`,
    nonEmptyString: (label: string) => `${label} must be a non-empty string`, onlyValues: (label: string, values: string) => `${label} must be one of ${values}`,
    exactlyOneSelector: (label: string) => `${label} must contain exactly one of name, glob, or preset`, invalidHeaderName: (label: string) => `${label} is not a valid header name`,
    invalidGlob: (label: string) => `${label} is not a valid glob`, removeHasValues: (label: string) => `${label} cannot contain values for Remove`, presetOnlyRemove: (label: string) => `${label} preset only supports Remove`,
    setNeedsValues: (label: string) => `${label} requires values for Set/Add`, hostValues: (label: string) => `${label} Host only supports single-value Set or Remove`,
    tokenPrefix: "Token must use the dproxy.12.i or dproxy.12.r format", tokenPayloadMissing: "Token payload is missing", tokenDecodeFailed: "Failed to decode Token payload", invalidRedisKey: "Redis key must be 1-256 bytes without surrounding whitespace or control characters",
    unknownField: (label: string, field: string) => `${label} contains unknown field ${field}`, pathRequired: "Request path is required",
    pathSameOrigin: "Request path must be a same-origin path starting with /", pathControlPlane: "Request path cannot target the control plane",
    headerValueString: (name: string) => `Request Header ${name} must be a string`,
  },
};

type Widen<T> = { [K in keyof T]: T[K] extends (...args: infer A) => unknown ? (...args: A) => string : T[K] extends object ? Widen<T[K]> : string };
export type Text = Widen<typeof zh>;

export function useText(): Text {
  const { locale } = useWorkbenchLocale();
  return locale === "en-US" ? en : zh;
}
