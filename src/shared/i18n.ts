import { useWorkbenchLocale } from "@lwmacct/260627-antd-workbench";

const zh = {
  app: {
    console: "控制台", settings: "设置", debugTools: "调试工具", preferences: "偏好设置",
    authConsole: "Directive 工作台", appearance: "外观设置",
  },
  auth: {
    authorizedOnly: "仅限已授权用户访问", signInDescription: "登录以继续",
    unavailable: "暂时无法连接认证服务", invalidToken: "访问令牌无效",
  },
  appearance: { panel: "主题与界面" },
  authConsole: {
    description: "完全在浏览器中通过 Builder、完整 Token JSON 或 Token 编辑 dp v18 directive。",
    structured: "Directive Builder", editableSources: "可编辑输入源", localOnly: "纯前端",
    directiveSource: "指令来源", inlineSource: "Inline", httpSource: "HTTP Remote", redisSource: "Redis Remote",
    basics: "基础", headers: "Headers", modules: "Modules", recovery: "Recovery",
    targetURL: "Target URL", proxyURL: "SOCKS5 Proxy URL", joinPath: "Join Path",
    redisKey: "Redis Key", optionalRemoteKey: "远程 Key（可选）", httpResolverURL: "HTTP Resolver URL", redisURL: "Redis URL",
    resolverHeaders: "Resolver 静态请求头", resolverRequestHeaders: "允许披露的原请求头", addResolverHeader: "添加请求头", removeResolverHeader: "删除 Resolver 请求头",
    enabled: "启用", disabled: "禁用", headerMode: "Header Mode", headerOps: "Header 操作", requestHeaderPolicy: "请求 Header", responseHeaderPolicy: "响应 Header", preserveProxyDisclosure: "保留代理披露 Header",
    add: "添加", op: "操作", match: "匹配", selector: "选择器", values: "值", set: "设置", remove: "移除", exact: "精确", valuePlaceholder: "输入值后按 Enter", removeHeaderOp: "删除 Header 操作",
    requestModules: "Request-scope Modules", attemptModules: "Attempt-scope Modules",
    requestModulesDescription: "Inline 与 Remote token 都可声明；在整个请求生命周期内保持稳定。",
    attemptModulesDescription: "仅属于 Inline payload；Remote 的 Attempt modules 由 resolver 返回的 payload 提供。",
    remoteAttemptModulesHint: "Remote token 不携带 Attempt-scope Modules；请在 HTTP/Redis 返回的裸 Payload 中配置。",
    moduleID: "ID", moduleName: "Module", moduleConfig: "Config JSON", addModule: "添加 Module", removeModule: "删除 Module", invalidModuleConfig: "Config 必须是有效 JSON",
    recoveryController: "Recovery Controller", recoveryDescription: "Controller、触发条件和预算都属于稳定 token envelope。", controller: "Controller",
    controllerURL: "Controller URL", controllerTimeout: "回调超时", controllerHeaders: "Controller 请求头", addControllerHeader: "添加 Controller 请求头", removeControllerHeader: "删除 Controller 请求头",
    triggers: "触发条件", responseHeaderTimeout: "响应头超时", errorTriggers: "错误触发器", transportError: "Transport Error", directiveError: "Directive Error",
    unexpectedStatus: "Unexpected Status", expectedStatusDescription: "填写视为正常响应的状态码范围；范围外的响应触发 Recovery。",
    rangeFrom: "状态码起点", rangeTo: "状态码终点", addStatusRange: "添加状态码范围", removeStatusRange: "删除状态码范围", captureBodyBytes: "捕获响应正文上限（bytes）",
    budget: "Recovery Budget", maxAttempts: "最大 Attempts", maxElapsed: "最大总时长",
    tokenJSON: "Token JSON", tokenJSONDescription: (prefix: string) => `这里编辑的是 ${prefix}.<base64url-json> 解码后的完整 JSON envelope，不是裸 Payload。`,
    dirty: "有未应用修改", synced: "已同步", copyJSON: "复制 JSON", copyToken: "复制 Token", applyJSON: "应用 JSON", parseToken: "解析 Token",
    jsonApplied: "Token JSON 已应用到 Builder 和 Token", jsonParseFailed: "Token JSON 解析失败", tokenApplied: "Token 已解析并应用到 Builder 和 JSON", tokenParseFailed: "Token 解析失败",
    invalidForm: "Builder 内容无效", invalidFormDetail: (detail: string) => `当前 Builder 无法生成 Token：${detail}`,
    requestDebug: "请求调试", requestDescription: "请求发送到当前站点的 data plane，并自动使用工作台本地生成的 Token。",
    cancel: "取消", send: "发起请求", bodyDisabled: (method: string) => `${method} 请求不会发送 Body`, waiting: "等待发起请求", requestCancelled: "请求已取消", requestFailed: "请求失败",
    copied: "已复制", copyFailed: "复制失败", directiveNotReady: "当前 Builder 或输入源尚未生成有效 Token",
    requestHeaders: "请求头 JSON", requestBody: "请求正文", responseHeaders: "响应头", responseBody: "响应正文",
    invalidJSON: (label: string) => `${label} 不是有效的 JSON`, mustBe: (label: string, type: string) => `${label} 必须是 ${type}`,
    nonEmptyString: (label: string) => `${label} 必须是非空字符串`, onlyValues: (label: string, values: string) => `${label} 只能是 ${values}`,
    exactlyOneSelector: (label: string) => `${label} 必须且只能包含 name 或 glob 之一`, invalidHeaderName: (label: string) => `${label} 不是合法的 Header 名`,
    invalidGlob: (label: string) => `${label} 不是合法的 Glob`, removeHasValues: (label: string) => `${label} Remove 操作不能包含 values`,
    setNeedsValues: (label: string) => `${label} Set/Add 操作必须包含 values`, hostValues: (label: string) => `${label} Host 只支持单值 Set 或 Remove`,
    tokenPrefix: "Token 必须使用 dp.18.inline/remote 格式", tokenDecodeFailed: "Token JSON Base64URL 解码失败", invalidRedisKey: "远程 Key 必须是 1-256 字节且不能包含首尾空白或控制字符", invalidResolverHeader: "请求头名称或值不合法",
    unknownField: (label: string, field: string) => `${label} 包含未知字段 ${field}`, pathRequired: "请求路径不能为空", pathSameOrigin: "请求路径必须是以 / 开头的同源路径", pathReservedAPI: "请求路径不能指向保留 API",
    headerValueString: (name: string) => `Request Header ${name} 的值必须是 string`,
  },
} as const;

const en: Text = {
  app: {
    console: "Console", settings: "Settings", debugTools: "Debug tools", preferences: "Preferences",
    authConsole: "Directive Workbench", appearance: "Appearance",
  },
  auth: {
    authorizedOnly: "Authorized users only", signInDescription: "Sign in to continue",
    unavailable: "Unable to connect to the authentication service", invalidToken: "Invalid access token",
  },
  appearance: { panel: "Theme and interface" },
  authConsole: {
    description: "Edit dp v18 directives entirely in the browser using the Builder, complete Token JSON, or Token.",
    structured: "Directive Builder", editableSources: "Editable sources", localOnly: "Browser only",
    directiveSource: "Directive source", inlineSource: "Inline", httpSource: "HTTP Remote", redisSource: "Redis Remote",
    basics: "Basics", headers: "Headers", modules: "Modules", recovery: "Recovery",
    targetURL: "Target URL", proxyURL: "SOCKS5 Proxy URL", joinPath: "Join Path",
    redisKey: "Redis key", optionalRemoteKey: "Remote key (optional)", httpResolverURL: "HTTP resolver URL", redisURL: "Redis URL",
    resolverHeaders: "Static resolver headers", resolverRequestHeaders: "Disclosed request headers", addResolverHeader: "Add header", removeResolverHeader: "Remove resolver header",
    enabled: "Enabled", disabled: "Disabled", headerMode: "Header Mode", headerOps: "Header operations", requestHeaderPolicy: "Request headers", responseHeaderPolicy: "Response headers", preserveProxyDisclosure: "Preserve proxy disclosure headers",
    add: "Add", op: "Op", match: "Match", selector: "Selector", values: "Values", set: "Set", remove: "Remove", exact: "Exact", valuePlaceholder: "Type a value and press Enter", removeHeaderOp: "Remove header operation",
    requestModules: "Request-scope Modules", attemptModules: "Attempt-scope Modules",
    requestModulesDescription: "Available to inline and remote tokens; stable for the entire request lifecycle.",
    attemptModulesDescription: "Part of an inline payload only; remote attempt modules come from the resolver payload.",
    remoteAttemptModulesHint: "Remote tokens do not carry Attempt-scope Modules. Configure them in the raw Payload returned by HTTP or Redis.",
    moduleID: "ID", moduleName: "Module", moduleConfig: "Config JSON", addModule: "Add Module", removeModule: "Remove Module", invalidModuleConfig: "Config must be valid JSON",
    recoveryController: "Recovery Controller", recoveryDescription: "The controller, triggers, and budget live in the stable token envelope.", controller: "Controller",
    controllerURL: "Controller URL", controllerTimeout: "Callback timeout", controllerHeaders: "Controller headers", addControllerHeader: "Add controller header", removeControllerHeader: "Remove controller header",
    triggers: "Triggers", responseHeaderTimeout: "Response header timeout", errorTriggers: "Error triggers", transportError: "Transport Error", directiveError: "Directive Error",
    unexpectedStatus: "Unexpected Status", expectedStatusDescription: "Define normal response status ranges; responses outside these ranges trigger Recovery.",
    rangeFrom: "Status range start", rangeTo: "Status range end", addStatusRange: "Add status range", removeStatusRange: "Remove status range", captureBodyBytes: "Captured response body limit (bytes)",
    budget: "Recovery Budget", maxAttempts: "Maximum Attempts", maxElapsed: "Maximum elapsed time",
    tokenJSON: "Token JSON", tokenJSONDescription: (prefix: string) => `This is the complete JSON envelope decoded from ${prefix}.<base64url-json>, not a raw Payload.`,
    dirty: "Unapplied changes", synced: "Synced", copyJSON: "Copy JSON", copyToken: "Copy Token", applyJSON: "Apply JSON", parseToken: "Parse Token",
    jsonApplied: "Token JSON applied to the Builder and Token", jsonParseFailed: "Failed to parse Token JSON", tokenApplied: "Token applied to the Builder and JSON", tokenParseFailed: "Failed to parse Token",
    invalidForm: "Invalid Builder state", invalidFormDetail: (detail: string) => `The Builder cannot generate a Token: ${detail}`,
    requestDebug: "Request debugger", requestDescription: "Send a request to this site's data plane using the Token generated locally by the workbench.",
    cancel: "Cancel", send: "Send request", bodyDisabled: (method: string) => `${method} requests do not send a body`, waiting: "Waiting to send a request", requestCancelled: "Request cancelled", requestFailed: "Request failed",
    copied: "Copied", copyFailed: "Copy failed", directiveNotReady: "The current Builder or source has not produced a valid Token",
    requestHeaders: "Request headers JSON", requestBody: "Request body", responseHeaders: "Response headers", responseBody: "Response body",
    invalidJSON: (label: string) => `${label} is not valid JSON`, mustBe: (label: string, type: string) => `${label} must be ${type}`,
    nonEmptyString: (label: string) => `${label} must be a non-empty string`, onlyValues: (label: string, values: string) => `${label} must be one of ${values}`,
    exactlyOneSelector: (label: string) => `${label} must contain exactly one of name or glob`, invalidHeaderName: (label: string) => `${label} is not a valid header name`,
    invalidGlob: (label: string) => `${label} is not a valid glob`, removeHasValues: (label: string) => `${label} cannot contain values for Remove`,
    setNeedsValues: (label: string) => `${label} requires values for Set/Add`, hostValues: (label: string) => `${label} Host only supports single-value Set or Remove`,
    tokenPrefix: "Token must use the dp.18.inline/remote format", tokenDecodeFailed: "Failed to decode Token JSON Base64URL", invalidRedisKey: "Remote key must be 1-256 bytes without surrounding whitespace or control characters", invalidResolverHeader: "Header name or value is invalid",
    unknownField: (label: string, field: string) => `${label} contains unknown field ${field}`, pathRequired: "Request path is required", pathSameOrigin: "Request path must be a same-origin path starting with /", pathReservedAPI: "Request path cannot target a reserved API",
    headerValueString: (name: string) => `Request Header ${name} must be a string`,
  },
};

type Widen<T> = { [K in keyof T]: T[K] extends (...args: infer A) => unknown ? (...args: A) => string : T[K] extends object ? Widen<T[K]> : string };
export type Text = Widen<typeof zh>;

export function useText(): Text {
  const { locale } = useWorkbenchLocale();
  return locale === "en-US" ? en : zh;
}
