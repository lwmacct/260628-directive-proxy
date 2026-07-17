export type DirectiveSource = "inline" | "http" | "redis";
export type TokenKind = "inline" | "remote";

export type ResolverHeader = {
  key: string;
  name: string;
  value: string;
};

export type HeaderOp = {
  key: string;
  side: "request" | "response";
  op: "set" | "del" | "add";
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
  source: DirectiveSource;
  remoteKey: string;
  httpURL: string;
  redisURL: string;
  resolverHeaderMode: "patch" | "replace";
  resolverPreserveProxyDisclosure: boolean;
  resolverHeaderOps: HeaderOp[];
  targetURL: string;
  joinPath: boolean;
  proxyURL: string;
  requestHeaderMode: "patch" | "replace";
  preserveProxyDisclosure: boolean;
  headerOps: HeaderOp[];
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
  target: { url: string; join_path?: boolean };
  proxy?: string;
  headers?: DirectiveHeaderPolicy;
  program?: DirectiveProgram;
  recovery?: RecoverySpec;
};

export type DirectiveHeaderOp = {
  side: "request" | "response";
  op: "set" | "del" | "add";
  name?: string;
  glob?: string;
  values?: string[];
};

export type DirectiveHeaderPolicy = {
  mode?: "patch" | "replace";
  preserve_proxy_disclosure?: boolean;
  ops?: DirectiveHeaderOp[];
};

export type RemoteSpec = {
  type: "http" | "redis";
  url: string;
  key?: string;
  headers?: DirectiveHeaderPolicy;
};

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
