import type { Text } from "../../shared/i18n";
import { newHeaderOp, newModuleSpec, newResolverHeader, newStatusRange } from "./constants";
import type {
  DirectiveEnvelope,
  DirectiveHeaderOp,
  DirectivePayload,
  EditorModuleSpec,
  EditorState,
  ModuleSpec,
  RecoveryEditorState,
  RecoverySpec,
  RemoteSpec,
  TokenKind,
} from "./types";

function buildHeaderOps(items: EditorState["requestHeaderOps"]): DirectiveHeaderOp[] {
  return items.flatMap<DirectiveHeaderOp>((item) => {
    const pattern = item.pattern.trim();
    if (!pattern) return [];
    const selector = item.selector === "name" ? { name: pattern } : { glob: pattern };
    return [{ op: item.op, ...selector, ...(item.op === "-" ? {} : { values: item.values }) }];
  });
}

function buildProgram(items: EditorModuleSpec[]): ModuleSpec[] {
  return items.map((item) => ({
    id: item.id,
    module: item.module,
    ...(item.config === undefined ? {} : { config: item.config }),
  }));
}

export function buildPayload(input: EditorState): DirectivePayload {
  const requestOps = buildHeaderOps(input.requestHeaderOps);
  const responseOps = buildHeaderOps(input.responseHeaderOps);
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
      ...(input.requestProgram.length ? { request: buildProgram(input.requestProgram) } : {}),
      ...(input.attemptProgram.length ? { attempt: buildProgram(input.attemptProgram) } : {}),
    };
  }
  return payload;
}

function buildHeaderMap(items: EditorState["resolverHeaders"]) {
  const entries = items.flatMap((header) => {
    const name = header.name.trim();
    return name ? [[name, header.value] as const] : [];
  });
  return Object.fromEntries(entries);
}

export function buildRemoteSpec(editor: EditorState): RemoteSpec {
  const headers = buildHeaderMap(editor.resolverHeaders);
  return {
    type: editor.source === "redis" ? "redis" : "http",
    url: (editor.source === "redis" ? editor.redisURL : editor.httpURL).trim(),
    ...(editor.remoteKey ? { key: editor.remoteKey } : {}),
    ...(editor.source === "http" && Object.keys(headers).length ? { headers } : {}),
    ...(editor.source === "http" && editor.resolverRequestHeaders.length ? { request_headers: editor.resolverRequestHeaders } : {}),
  };
}

export function buildRecovery(input: RecoveryEditorState): RecoverySpec | undefined {
  if (!input.enabled) return undefined;
  const headers = buildHeaderMap(input.controllerHeaders);
  return {
    controller: {
      url: input.controllerURL.trim(),
      ...(Object.keys(headers).length ? { headers } : {}),
      ...(input.controllerTimeout.trim() ? { timeout: input.controllerTimeout.trim() } : {}),
    },
    triggers: {
      ...(input.responseHeaderTimeout.trim() ? { response_header_timeout: input.responseHeaderTimeout.trim() } : {}),
      ...(input.unexpectedStatusEnabled ? {
        unexpected_status: {
          expected: input.expectedStatuses.map((range) => ({ from: range.from, to: range.to })),
          ...(input.captureBodyBytes === undefined ? {} : { capture_body_bytes: input.captureBodyBytes }),
        },
      } : {}),
      ...(input.transportError ? { transport_error: true } : {}),
      ...(input.directiveError ? { directive_error: true } : {}),
    },
    budget: {
      max_attempts: input.maxAttempts,
      ...(input.maxElapsed.trim() ? { max_elapsed: input.maxElapsed.trim() } : {}),
    },
  };
}

export function buildEnvelope(editor: EditorState): DirectiveEnvelope {
  const recovery = buildRecovery(editor.recovery);
  if (editor.source === "inline") {
    return {
      kind: "inline",
      document: { payload: buildPayload(editor), ...(recovery ? { recovery } : {}) },
    };
  }
  return {
    kind: "remote",
    document: {
      source: buildRemoteSpec(editor),
      ...(editor.requestProgram.length ? { program: { request: buildProgram(editor.requestProgram) } } : {}),
      ...(recovery ? { recovery } : {}),
    },
  };
}

function payloadToEditor(payload: DirectivePayload) {
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
    requestProgram: (payload.program?.request ?? []).map((item) => newModuleSpec(item.id, item.module, item.config ?? {})),
    attemptProgram: (payload.program?.attempt ?? []).map((item) => newModuleSpec(item.id, item.module, item.config ?? {})),
  };
}

function recoveryToEditor(previous: RecoveryEditorState, recovery?: RecoverySpec): RecoveryEditorState {
  if (!recovery) return { ...previous, enabled: false };
  return {
    enabled: true,
    controllerURL: recovery.controller.url,
    controllerTimeout: recovery.controller.timeout ?? "3s",
    controllerHeaders: Object.entries(recovery.controller.headers ?? {}).map(([name, value]) => newResolverHeader(name, value)),
    responseHeaderTimeout: recovery.triggers.response_header_timeout ?? "",
    unexpectedStatusEnabled: recovery.triggers.unexpected_status !== undefined,
    expectedStatuses: (recovery.triggers.unexpected_status?.expected ?? [{ from: 200, to: 299 }]).map((range) => newStatusRange(range.from, range.to)),
    captureBodyBytes: recovery.triggers.unexpected_status?.capture_body_bytes ?? 65536,
    transportError: recovery.triggers.transport_error ?? false,
    directiveError: recovery.triggers.directive_error ?? false,
    maxAttempts: recovery.budget.max_attempts,
    maxElapsed: recovery.budget.max_elapsed ?? "30s",
  };
}

export function envelopeToEditor(previous: EditorState, envelope: DirectiveEnvelope): EditorState {
  if (envelope.kind === "inline") {
    return {
      ...previous,
      source: "inline",
      ...payloadToEditor(envelope.document.payload),
      recovery: recoveryToEditor(previous.recovery, envelope.document.recovery),
    };
  }
  const spec = envelope.document.source;
  return {
    ...previous,
    source: spec.type,
    remoteKey: spec.key ?? "",
    requestProgram: (envelope.document.program?.request ?? []).map((item) => newModuleSpec(item.id, item.module, item.config ?? {})),
    attemptProgram: [],
    recovery: recoveryToEditor(previous.recovery, envelope.document.recovery),
    ...(spec.type === "http"
      ? {
          httpURL: spec.url,
          resolverHeaders: Object.entries(spec.headers ?? {}).map(([name, value]) => newResolverHeader(name, value)),
          resolverRequestHeaders: spec.request_headers ?? [],
        }
      : { redisURL: spec.url }),
  };
}

export function sourceTokenKind(source: EditorState["source"]): TokenKind {
  return source === "inline" ? "inline" : "remote";
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function normalizeRequestPath(value: string, text: Text["authConsole"]) {
  const path = value.trim();
  if (!path) throw new Error(text.pathRequired);
  if (!path.startsWith("/") || path.startsWith("//")) throw new Error(text.pathSameOrigin);
  if (path === "/health" || path === "/authme" || path.startsWith("/authme/") || path === "/api/admin" || path.startsWith("/api/admin/") || path === "/api/public" || path.startsWith("/api/public/")) {
    throw new Error(text.pathReservedAPI);
  }
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
