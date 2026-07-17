import type {
  EditorModuleSpec,
  EditorState,
  HeaderOp,
  RecoveryEditorState,
  ResolverHeader,
  StatusRange,
} from "./types";

let rowID = 0;

function nextKey(prefix: string) {
  rowID += 1;
  return `${prefix}-${rowID}`;
}

export function newHeaderOp(
  op: HeaderOp["op"],
  selector: HeaderOp["selector"],
  pattern: string,
  values: string[],
): HeaderOp {
  return { key: nextKey("header-op"), op, selector, pattern, values };
}

export function newResolverHeader(name: string, value: string): ResolverHeader {
  return { key: nextKey("header"), name, value };
}

export function newModuleSpec(id = "", module = "", config: unknown = {}): EditorModuleSpec {
  return { key: nextKey("module"), id, module, config, configText: JSON.stringify(config, null, 2), configValid: true };
}

export function newStatusRange(from = 200, to = 299): StatusRange {
  return { key: nextKey("status-range"), from, to };
}

export function initialRecovery(): RecoveryEditorState {
  return {
    enabled: false,
    controllerURL: "https://controller.example.com/recovery",
    controllerTimeout: "3s",
    controllerHeaders: [],
    responseHeaderTimeout: "",
    unexpectedStatusEnabled: true,
    expectedStatuses: [newStatusRange()],
    captureBodyBytes: 65536,
    transportError: true,
    directiveError: false,
    maxAttempts: 3,
    maxElapsed: "30s",
  };
}

export const initialEditor: EditorState = {
  source: "inline",
  remoteKey: "team-a/service-a",
  httpURL: "https://policy.example.com/v1/resolve",
  redisURL: "redis://user:password@redis.example.com:6379/1",
  resolverHeaders: [newResolverHeader("Authorization", "Bearer policy-token")],
  resolverRequestHeaders: ["Content-Type", "X-Tenant"],
  targetURL: "https://httpbin.org/anything",
  joinPath: true,
  proxyURL: "",
  requestHeaderMode: "patch",
  preserveProxyDisclosure: false,
  requestHeaderOps: [
    newHeaderOp("=", "name", "Authorization", ["Bearer upstream-token"]),
    newHeaderOp("=", "name", "X-Dproxy-Key", ["dproxy-demo-key"]),
  ],
  responseHeaderOps: [],
  requestProgram: [newModuleSpec("capture", "builtin.capture")],
  attemptProgram: [],
  recovery: initialRecovery(),
};
