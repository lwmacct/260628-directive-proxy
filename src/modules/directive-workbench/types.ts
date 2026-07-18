export type DirectiveSource = "inline" | "http" | "redis" | "file";
export type TokenKind = "inline" | "remote";

export type ResolverHeader = {
  key: string;
  name: string;
  value: string;
};

export type MetadataField = ResolverHeader;

export type HeaderMutation = {
  key: string;
  side: "request" | "response";
  action: "set" | "remove" | "append";
  selector: "name" | "glob";
  pattern: string;
  values: string[];
};

export type EditorModuleSpec = {
  key: string;
  id: string;
  module: string;
  config?: unknown;
  configText: string;
  configValid: boolean;
};

export type StatusRange = {
  key: string;
  from: number;
  to: number;
};

export type RecoveryEditorState = {
  enabled: boolean;
  controllerURL: string;
  controllerTimeout: string;
  controllerHeaders: ResolverHeader[];
  responseHeaderTimeout: string;
  unexpectedStatusEnabled: boolean;
  expectedStatuses: StatusRange[];
  captureBodyBytes?: number;
  transportError: boolean;
  maxAttempts: number;
  maxElapsed: string;
};

export type EditorState = {
  remoteKey: string;
  httpURL: string;
  redisURL: string;
  filePath: string;
  resolverHeaderMode: "patch" | "replace";
  resolverPreserveProxyDisclosure: boolean;
  resolverHeaderMutations: HeaderMutation[];
  metadataFields: MetadataField[];
  targetMode: "base" | "exact";
  targetURL: string;
  proxyURL: string;
  requestHeaderMode: "patch" | "replace";
  preserveProxyDisclosure: boolean;
  headerMutations: HeaderMutation[];
  requestProgram: EditorModuleSpec[];
  attemptProgram: EditorModuleSpec[];
  recovery: RecoveryEditorState;
};

export type ModuleSpec = {
  id: string;
  module: string;
  config?: unknown;
};

export type DirectiveProgram = {
  request?: ModuleSpec[];
  attempt?: ModuleSpec[];
};

export type DirectivePayload = {
  metadata?: Record<string, string>;
  target: { base_url: string } | { exact_url: string };
  proxy?: string;
  headers?: DirectiveHeaderPolicy;
  program?: DirectiveProgram;
  recovery?: RecoverySpec;
};

export type DirectiveHeaderMutation = {
  side: "request" | "response";
  action: "set" | "remove" | "append";
  name?: string;
  glob?: string;
  values?: string[];
};

export type DirectiveHeaderPolicy = {
  mode?: "patch" | "replace";
  preserve_proxy_disclosure?: boolean;
  mutations?: DirectiveHeaderMutation[];
};

export type RemoteSpec =
  | { http: { url: string; headers?: DirectiveHeaderPolicy } }
  | { redis: { url: string; key: string } }
  | { file: { path: string } };

export type RecoverySpec = {
  controller: {
    url: string;
    headers?: Record<string, string>;
    timeout?: string;
  };
  triggers: {
    response_header_timeout?: string;
    unexpected_status?: {
      expected: Array<{ from: number; to: number }>;
      capture_body_bytes?: number;
    };
    transport_error?: boolean;
  };
  budget: {
    max_attempts: number;
    max_elapsed?: string;
  };
};

export type DirectiveEnvelope =
  | { kind: "inline"; document: DirectivePayload }
  | { kind: "remote"; document: RemoteSpec };

export type RequestResult = {
  body: string;
  duration: number;
  headers: string;
  status: number;
  statusText: string;
};
