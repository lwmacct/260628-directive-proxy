export type DirectiveSource = "inline" | "http" | "redis";

export type ResolverHeader = {
  key: string;
  name: string;
  value: string;
};

export type HeaderOp = {
  key: string;
  op: "=" | "+" | "-";
  selector: "name" | "glob" | "preset";
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
  headerMode: "patch" | "replace";
  headerOps: HeaderOp[];
};

export type DirectivePayload = {
  target: { url: string; join_path?: boolean };
  proxy?: string;
  headers?: {
    mode?: "patch" | "replace";
    ops?: Array<{
      op: "=" | "+" | "-";
      name?: string;
      glob?: string;
      preset?: "proxy-disclosure";
      values?: string[];
    }>;
  };
};

export type DirectiveHeaderOp = NonNullable<NonNullable<DirectivePayload["headers"]>["ops"]>[number];

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
