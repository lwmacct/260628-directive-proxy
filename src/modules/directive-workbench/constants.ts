import type { EditorState, HeaderOp, ResolverHeader } from "./types";

let headerOpID = 0;
let resolverHeaderID = 0;

export function newHeaderOp(
  op: HeaderOp["op"],
  selector: HeaderOp["selector"],
  pattern: string,
  values: string[],
): HeaderOp {
  headerOpID += 1;
  return { key: `header-op-${headerOpID}`, op, selector, pattern, values };
}

export function newResolverHeader(name: string, value: string): ResolverHeader {
  resolverHeaderID += 1;
  return { key: `resolver-header-${resolverHeaderID}`, name, value };
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
  requestProgram: [{ id: "capture", module: "builtin.capture", config: {} }],
  attemptProgram: [],
  recovery: undefined,
};
