import type { Text } from "../../shared/i18n";
import { newHeaderMutation, newModuleSpec, newResolverHeader, newStatusRange } from "./constants";
import type {
  DirectiveEnvelope,
  DirectiveSource,
  DirectiveHeaderMutation,
  DirectivePayload,
  EditorModuleSpec,
  EditorState,
  HeaderMutation,
  ModuleSpec,
  RecoveryEditorState,
  RecoverySpec,
  RemoteSpec,
  ResolverHeader,
  TokenKind,
} from "./types";

function buildHeaderMutations(items: HeaderMutation[]): DirectiveHeaderMutation[] {
  return items.flatMap<DirectiveHeaderMutation>((item) => {
    const pattern = item.pattern.trim();
    if (!pattern) return [];
    const selector = item.selector === "name" ? { name: pattern } : { glob: pattern };
    return [{ side: item.side, action: item.action, ...selector, ...(item.action === "remove" ? {} : { values: item.values }) }];
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
  const mutations = buildHeaderMutations(input.headerMutations);
  const targetURL = input.targetURL.trim();
  const payload: DirectivePayload = {
    target: input.targetMode === "base" ? { base_url: targetURL } : { exact_url: targetURL },
  };
  if (input.proxyURL.trim()) payload.proxy = input.proxyURL.trim();
  const headers = {
    ...(input.requestHeaderMode === "replace" ? { mode: input.requestHeaderMode } : {}),
    ...(input.preserveProxyDisclosure ? { preserve_proxy_disclosure: true } : {}),
    ...(mutations.length ? { mutations } : {}),
  };
  if (Object.keys(headers).length) payload.headers = headers;
  if (input.requestProgram.length || input.attemptProgram.length) {
    payload.program = {
      ...(input.requestProgram.length ? { request: buildProgram(input.requestProgram) } : {}),
      ...(input.attemptProgram.length ? { attempt: buildProgram(input.attemptProgram) } : {}),
    };
  }
  const recovery = buildRecovery(input.recovery);
  if (recovery) payload.recovery = recovery;
  return payload;
}

function buildHeaderMap(items: ResolverHeader[]) {
  const entries = items.flatMap((header) => {
    const name = header.name.trim();
    return name ? [[name, header.value] as const] : [];
  });
  return Object.fromEntries(entries);
}

export function buildRemoteSpec(source: Exclude<DirectiveSource, "inline">, editor: EditorState): RemoteSpec {
  if (source === "file") return { file: { path: editor.filePath.trim() } };
  if (source === "redis") return { redis: { url: editor.redisURL.trim(), key: editor.remoteKey } };
  const mutations = buildHeaderMutations(editor.resolverHeaderMutations);
  const headers = {
    ...(editor.resolverHeaderMode === "replace" ? { mode: "replace" as const } : {}),
    ...(editor.resolverPreserveProxyDisclosure ? { preserve_proxy_disclosure: true } : {}),
    ...(mutations.length ? { mutations } : {}),
  };
  return {
    http: {
      url: editor.httpURL.trim(),
      ...(Object.keys(headers).length ? { headers } : {}),
    },
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
    },
    budget: {
      max_attempts: input.maxAttempts,
      ...(input.maxElapsed.trim() ? { max_elapsed: input.maxElapsed.trim() } : {}),
    },
  };
}

export function buildEnvelope(source: DirectiveSource, editor: EditorState): DirectiveEnvelope {
  if (source === "inline") {
    return { kind: "inline", document: buildPayload(editor) };
  }
  return { kind: "remote", document: buildRemoteSpec(source, editor) };
}

function payloadToEditor(payload: DirectivePayload) {
  return {
    targetMode: "base_url" in payload.target ? "base" as const : "exact" as const,
    targetURL: "base_url" in payload.target ? payload.target.base_url : payload.target.exact_url,
    proxyURL: payload.proxy ?? "",
    requestHeaderMode: payload.headers?.mode ?? "patch",
    preserveProxyDisclosure: payload.headers?.preserve_proxy_disclosure ?? false,
    headerMutations: toEditorHeaderMutations(payload.headers?.mutations ?? []),
    requestProgram: (payload.program?.request ?? []).map((item) => newModuleSpec(item.id, item.module, item.config ?? {})),
    attemptProgram: (payload.program?.attempt ?? []).map((item) => newModuleSpec(item.id, item.module, item.config ?? {})),
    recovery: payload.recovery,
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
    maxAttempts: recovery.budget.max_attempts,
    maxElapsed: recovery.budget.max_elapsed ?? "30s",
  };
}

export function envelopeToEditor(previous: EditorState, envelope: DirectiveEnvelope): EditorState {
  if (envelope.kind === "inline") {
    const payload = envelope.document;
    const parsed = payloadToEditor(payload);
    return {
      ...previous,
      ...parsed,
      recovery: recoveryToEditor(previous.recovery, parsed.recovery),
    };
  }
  const spec = envelope.document;
  if ("file" in spec) {
    return { ...previous, filePath: spec.file.path };
  }
  if ("redis" in spec) {
    return { ...previous, redisURL: spec.redis.url, remoteKey: spec.redis.key };
  }
  return {
    ...previous,
    httpURL: spec.http.url,
    resolverHeaderMode: spec.http.headers?.mode ?? "patch",
    resolverPreserveProxyDisclosure: spec.http.headers?.preserve_proxy_disclosure ?? false,
    resolverHeaderMutations: toEditorHeaderMutations(spec.http.headers?.mutations ?? []),
  };
}

function toEditorHeaderMutations(mutations: DirectiveHeaderMutation[]) {
  return mutations.map((item) => newHeaderMutation(
    item.action,
    item.glob !== undefined ? "glob" : "name",
    item.name ?? item.glob ?? "",
    item.values ?? [],
    item.side,
  ));
}

export function envelopeSource(envelope: DirectiveEnvelope): DirectiveSource {
  if (envelope.kind === "inline") return "inline";
  if ("file" in envelope.document) return "file";
  if ("redis" in envelope.document) return "redis";
  return "http";
}

export function sourceTokenKind(source: DirectiveSource): TokenKind {
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
