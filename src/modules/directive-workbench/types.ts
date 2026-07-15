export type DirectiveSource = "inline" | "http" | "redis";

export type ResolverHeader = {
  key: string;
  name: string;
  value: string;
};

export type HeaderOp = {
  key: string;
  op: "=" | "+" | "-";
  selector: "name" | "glob";
  pattern: string;
  values: string[];
};

export type EditorState = {
  source: DirectiveSource;
  remoteKey: string;
  httpURL: string;
  redisURL: string;
  resolverHeaders: ResolverHeader[];
  resolverRequestHeaders: string[];
  targetURL: string;
  joinPath: boolean;
  proxyURL: string;
  requestHeaderMode: "patch" | "replace";
  preserveProxyDisclosure: boolean;
  requestHeaderOps: HeaderOp[];
  responseHeaderOps: HeaderOp[];
};

export type DirectivePayload = {
  target: { url: string; join_path?: boolean };
  proxy?: string;
  headers?: {
    request?: {
      mode?: "patch" | "replace";
      preserve_proxy_disclosure?: boolean;
      ops?: DirectiveHeaderOp[];
    };
    response?: { ops?: DirectiveHeaderOp[] };
  };
};

export type DirectiveHeaderOp = {
  op: "=" | "+" | "-";
  name?: string;
  glob?: string;
  values?: string[];
};

export type RemoteSpec = {
  type: "http" | "redis";
  url: string;
  key?: string;
  headers?: Record<string, string>;
  request_headers?: string[];
};

export type DirectiveDocument =
  | { kind: "inline"; payload: DirectivePayload }
  | { kind: "remote"; remote: RemoteSpec };

export type DirectiveCodecResponse = {
  token: string;
  document: DirectiveDocument;
};

export type RequestResult = {
  body: string;
  duration: number;
  headers: string;
  status: number;
  statusText: string;
};
