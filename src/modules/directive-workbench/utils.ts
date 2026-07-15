import type { Text } from "../../shared/i18n";
import { newHeaderOp, newResolverHeader } from "./constants";
import type { DirectiveDocument, DirectiveHeaderOp, DirectivePayload, EditorState, RemoteSpec } from "./types";

export function buildPayload(input: EditorState): DirectivePayload {
  const ops = input.headerOps.flatMap<DirectiveHeaderOp>((item) => {
    const pattern = item.pattern.trim();
    if (!pattern) return [];
    const selector = item.selector === "name"
      ? { name: pattern }
      : item.selector === "glob"
        ? { glob: pattern }
        : { preset: pattern as "proxy-disclosure" };
    return [{ op: item.op, ...selector, ...(item.op === "-" ? {} : { values: item.values }) }];
  });
  const payload: DirectivePayload = { target: { url: input.targetURL.trim() } };
  if (!input.joinPath) payload.target.join_path = false;
  if (input.proxyURL.trim()) payload.proxy = input.proxyURL.trim();
  if (ops.length > 0) payload.headers = { mode: input.headerMode, ops };
  return payload;
}

export function payloadToEditor(payload: DirectivePayload): Pick<EditorState, "targetURL" | "joinPath" | "proxyURL" | "headerMode" | "headerOps"> {
  return {
    targetURL: payload.target.url,
    joinPath: payload.target.join_path ?? true,
    proxyURL: payload.proxy ?? "",
    headerMode: payload.headers?.mode ?? "patch",
    headerOps: (payload.headers?.ops ?? []).map((item) => newHeaderOp(
      item.op,
      item.preset !== undefined ? "preset" : item.glob !== undefined ? "glob" : "name",
      item.name ?? item.glob ?? item.preset ?? "",
      item.values ?? [],
    )),
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

export function remoteSpecToEditor(editor: EditorState, spec: RemoteSpec): EditorState {
  return {
    ...editor,
    source: spec.type,
    remoteKey: spec.key ?? "",
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
  return editor.source === "inline" ? { kind: "inline", payload } : { kind: "remote", remote: buildRemoteSpec(editor) };
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
