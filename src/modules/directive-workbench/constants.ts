import type {
  EditorModuleSpec,
  EditorState,
  HeaderMutation,
  RecoveryEditorState,
  ResolverHeader,
  StatusRange,
} from "./types";

let rowID = 0;

function nextKey(prefix: string) {
  rowID += 1;
  return `${prefix}-${rowID}`;
}

export function newHeaderMutation(
  action: HeaderMutation["action"],
  selector: HeaderMutation["selector"],
  pattern: string,
  values: string[],
  side: HeaderMutation["side"] = "request",
): HeaderMutation {
  return { key: nextKey("header-mutation"), side, action, selector, pattern, values };
}

export function newResolverHeader(name: string, value: string): ResolverHeader {
  return { key: nextKey("header"), name, value };
}

export function newModuleSpec(module = "", config: unknown = {}): EditorModuleSpec {
  return { key: nextKey("module"), module, config, configText: JSON.stringify(config, null, 2), configValid: true };
}

export function newStatusRange(from = 200, to = 299): StatusRange {
  return { key: nextKey("status-range"), from, to };
}

export function initialRecovery(): RecoveryEditorState {
  return {
    enabled: false,
    controllerURL: "https://controller.example.com/recovery",
    controllerHeaders: [],
    controllerTimeout: "3s",
    responseHeaderTimeout: "",
    unexpectedStatusEnabled: true,
    expectedStatuses: [newStatusRange()],
    captureBodyBytes: 65536,
    transportError: true,
    maxRoundTrips: 3,
    maxElapsed: "30s",
  };
}

export function createInitialEditor(): EditorState {
  return {
    remoteKey: "team-a/service-a",
    httpURL: "https://policy.example.com/v1/resolve",
    redisURL: "redis://user:password@redis.example.com:6379/1",
    filePath: "team-a/services/primary.json",
    resolverHeaderMode: "patch",
    resolverPreserveProxyDisclosure: false,
    resolverHeaderMutations: [newHeaderMutation("set", "name", "Authorization", ["Bearer policy-token"])],
    metadataFields: [],
    targetMode: "base",
    targetURL: "https://httpbin.org/anything",
    proxyURL: "",
    requestHeaderMode: "patch",
    preserveProxyDisclosure: false,
    headerMutations: [
      newHeaderMutation("set", "name", "Authorization", ["Bearer upstream-token"]),
      newHeaderMutation("set", "name", "X-Api-Key", ["demo-api-key"]),
    ],
    modules: [newModuleSpec("builtin.capture")],
    recovery: initialRecovery(),
  };
}
