import type { Text } from "../../shared/i18n";
import { newHeaderOp, newResolverHeader } from "./constants";
import type { DirectiveDocument, DirectiveHeaderOp, DirectivePayload, EditorState, RemoteDocument, RemoteSpec } from "./types";

export function buildPayload(input: EditorState): DirectivePayload {
  const buildOps = (items: EditorState["requestHeaderOps"]): DirectiveHeaderOp[] => items.flatMap<DirectiveHeaderOp>((item) => {
    const pattern = item.pattern.trim();
    if (!pattern) return [];
    const selector = item.selector === "name" ? { name: pattern } : { glob: pattern };
    return [{ op: item.op, ...selector, ...(item.op === "-" ? {} : { values: item.values }) }];
  });
  const requestOps = buildOps(input.requestHeaderOps);
  const responseOps = buildOps(input.responseHeaderOps);
  const payload: DirectivePayload = { target: { url: input.targetURL.trim() } };
  if (!input.joinPath) payload.target.join_path = false;
  if (input.proxyURL.trim()) payload.proxy = input.proxyURL.trim();
  const request = {
    ...(input.requestHeaderMode === "replace" ? { mode: input.requestHeaderMode } : {}),
    ...(input.preserveProxyDisclosure ? { preserve_proxy_disclosure: true } : {}),
    ...(requestOps.length ? { ops: requestOps } : {}),
  };
  if (Object.keys(request).length || responseOps.length) {
    payload.headers = {
      ...(Object.keys(request).length ? { request } : {}),
      ...(responseOps.length ? { response: { ops: responseOps } } : {}),
    };
  }
  if (input.requestProgram.length || input.attemptProgram.length) {
    payload.program = {
      ...(input.requestProgram.length ? { request: input.requestProgram } : {}),
      ...(input.attemptProgram.length ? { attempt: input.attemptProgram } : {}),
    };
  }
  return payload;
}

export function payloadToEditor(payload: DirectivePayload): Pick<EditorState, "targetURL" | "joinPath" | "proxyURL" | "requestHeaderMode" | "preserveProxyDisclosure" | "requestHeaderOps" | "responseHeaderOps" | "requestProgram" | "attemptProgram"> {
  const toEditorOps = (ops: DirectiveHeaderOp[]) => ops.map((item) => newHeaderOp(
    item.op,
    item.glob !== undefined ? "glob" : "name",
    item.name ?? item.glob ?? "",
    item.values ?? [],
  ));
  return {
    targetURL: payload.target.url,
    joinPath: payload.target.join_path ?? true,
    proxyURL: payload.proxy ?? "",
    requestHeaderMode: payload.headers?.request?.mode ?? "patch",
    preserveProxyDisclosure: payload.headers?.request?.preserve_proxy_disclosure ?? false,
    requestHeaderOps: toEditorOps(payload.headers?.request?.ops ?? []),
    responseHeaderOps: toEditorOps(payload.headers?.response?.ops ?? []),
    requestProgram: payload.program?.request ?? [],
    attemptProgram: payload.program?.attempt ?? [],
  };
}

export function buildRemoteSpec(editor: EditorState): RemoteSpec {
  const headers = Object.fromEntries(editor.resolverHeaders.flatMap((header) => {
    const name = header.name.trim();
    return name ? [[name, header.value]] : [];
  }));
  return {
    type: editor.source === "redis" ? "redis" : "http",
    url: (editor.source === "redis" ? editor.redisURL : editor.httpURL).trim(),
    ...(editor.remoteKey ? { key: editor.remoteKey } : {}),
    ...(editor.source === "http" && Object.keys(headers).length ? { headers } : {}),
    ...(editor.source === "http" && editor.resolverRequestHeaders.length ? { request_headers: editor.resolverRequestHeaders } : {}),
  };
}

export function remoteDocumentToEditor(editor: EditorState, remote: RemoteDocument): EditorState {
  const spec = remote.source;
  return {
    ...editor,
    source: spec.type,
    remoteKey: spec.key ?? "",
    requestProgram: remote.program?.request ?? [],
    attemptProgram: [],
    ...(spec.type === "http"
      ? {
          httpURL: spec.url,
          resolverHeaders: Object.entries(spec.headers ?? {}).map(([name, value]) => newResolverHeader(name, value)),
          resolverRequestHeaders: spec.request_headers ?? [],
        }
      : { redisURL: spec.url }),
  };
}

export function encodeDocument(editor: EditorState, payload: DirectivePayload): DirectiveDocument {
  const recovery = editor.recovery ? { recovery: editor.recovery } : {};
  return editor.source === "inline" ? { kind: "inline", payload, ...recovery } : {
    kind: "remote",
    remote: {
      source: buildRemoteSpec(editor),
      ...(editor.requestProgram.length ? { program: { request: editor.requestProgram } } : {}),
    },
    ...recovery,
  };
}

export function isRemoteKeyValid(value: string) {
  const bytes = new TextEncoder().encode(value);
  return Boolean(value) && value === value.trim() && bytes.length <= 256 && ![...value].some((char) => {
    const codePoint = char.codePointAt(0) ?? 0;
    return codePoint === 0 || codePoint < 0x20 || codePoint === 0x7f;
  });
}

export function formatPayload(payload: DirectivePayload) {
  return JSON.stringify(payload, null, 2);
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function normalizeRequestPath(value: string, text: Text["authConsole"]) {
  const path = value.trim();
  if (!path) throw new Error(text.pathRequired);
  if (!path.startsWith("/") || path.startsWith("//")) throw new Error(text.pathSameOrigin);
  if (path.startsWith("/api/") || path === "/api" || path === "/health") throw new Error(text.pathReservedAPI);
  return path;
}

export function parseRequestHeaders(value: string, text: Text["authConsole"]): Record<string, string> {
  let parsed: unknown;
  try { parsed = JSON.parse(value || "{}"); } catch { throw new Error(text.invalidJSON("Request Headers")); }
  if (!isRecord(parsed)) throw new Error(text.mustBe("Request Headers", "JSON object"));
  const headers: Record<string, string> = {};
  for (const [name, headerValue] of Object.entries(parsed)) {
    if (typeof headerValue !== "string") throw new Error(text.headerValueString(name));
    if (name.toLowerCase() !== "authorization") headers[name] = headerValue;
  }
  return headers;
}

export function formatResponseHeaders(headers: Headers) {
  return [...headers.entries()].map(([name, value]) => `${name}: ${value}`).join("\n");
}

export function statusColor(status: number) {
  if (status >= 500) return "red";
  if (status >= 400) return "orange";
  if (status >= 300) return "blue";
  if (status >= 200) return "green";
  return "default";
}

export function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

export async function copyText(value: string) {
  if (navigator.clipboard?.writeText && window.isSecureContext) {
    try { await navigator.clipboard.writeText(value); return true; } catch { /* Use the legacy path below. */ }
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.select();
  try { return document.execCommand("copy"); } finally { document.body.removeChild(textarea); }
}
