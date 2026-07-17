import type { Text } from "../../shared/i18n";
import type {
  DirectiveEnvelope,
  DirectiveHeaderMutation,
  DirectivePayload,
  DirectiveProgram,
  ModuleSpec,
  RecoverySpec,
  RemoteSpec,
  TokenKind,
} from "./types";

const forbiddenHeaders = new Set([
  "connection",
  "content-length",
  "content-type",
  "host",
  "keep-alive",
  "proxy-connection",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
]);

const protectedResponseHeaders = new Set([
  "connection",
  "content-length",
  "date",
  "dproxy-retry-id",
  "host",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "proxy-connection",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function record(value: unknown, label: string, text: Text["authConsole"]) {
  if (!isRecord(value)) throw new Error(text.mustBe(label, "JSON object"));
  return value;
}

function knownKeys(value: Record<string, unknown>, allowed: readonly string[], label: string, text: Text["authConsole"]) {
  const allowedSet = new Set(allowed);
  const unknown = Object.keys(value).find((key) => !allowedSet.has(key));
  if (unknown) throw new Error(text.unknownField(label, unknown));
}

function stringValue(value: unknown, label: string, text: Text["authConsole"], required = true) {
  if (typeof value !== "string" || required && value.trim() === "") throw new Error(text.nonEmptyString(label));
  return value.trim();
}

function optionalString(value: unknown, label: string, text: Text["authConsole"]) {
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw new Error(text.mustBe(label, "string"));
  const result = value.trim();
  return result || undefined;
}

function booleanValue(value: unknown, label: string, text: Text["authConsole"]) {
  if (typeof value !== "boolean") throw new Error(text.mustBe(label, "boolean"));
  return value;
}

function integerValue(value: unknown, label: string, text: Text["authConsole"], min: number, max: number) {
  if (typeof value !== "number" || !Number.isInteger(value) || value < min || value > max) {
    throw new Error(text.mustBe(label, `integer ${min}-${max}`));
  }
  return value;
}

function arrayValue(value: unknown, label: string, text: Text["authConsole"]) {
  if (!Array.isArray(value)) throw new Error(text.mustBe(label, "array"));
  return value;
}

function parseURL(value: unknown, label: string, schemes: string[], text: Text["authConsole"], userInfo = true, fragment = true) {
  const raw = stringValue(value, label, text);
  let parsed: URL;
  try { parsed = new URL(raw); } catch { throw new Error(text.mustBe(label, `${schemes.join("/")} URL`)); }
  const disallowedUserInfo = !userInfo && Boolean(parsed.username || parsed.password);
  const disallowedFragment = !fragment && Boolean(parsed.hash);
  if (!schemes.includes(parsed.protocol.slice(0, -1)) || !parsed.hostname || disallowedUserInfo || disallowedFragment) {
    throw new Error(text.mustBe(label, `${schemes.join("/")} URL`));
  }
  return raw;
}

function isHeaderName(value: string) {
  return /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(value);
}

function isHeaderValue(value: string) {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code !== 0x09 && (code < 0x20 || code === 0x7f)) return false;
  }
  return true;
}

function canonicalHeaderName(value: string) {
  return value.toLowerCase().split("-").map((part) => part ? part[0].toUpperCase() + part.slice(1) : part).join("-");
}

function parseHeaderMap(
  value: unknown,
  label: string,
  text: Text["authConsole"],
  maxCount?: number,
  maxValueBytes?: number,
) {
  if (value === undefined) return undefined;
  const input = record(value, label, text);
  if (maxCount !== undefined && Object.keys(input).length > maxCount) throw new Error(text.mustBe(label, `object with at most ${maxCount} entries`));
  const output: Record<string, string> = {};
  const names = new Set<string>();
  for (const [rawName, rawValue] of Object.entries(input)) {
    const name = rawName.trim();
    const lower = name.toLowerCase();
    if (!isHeaderName(name) || forbiddenHeaders.has(lower) || typeof rawValue !== "string" || /[\r\n]/.test(rawValue) || maxValueBytes !== undefined && new TextEncoder().encode(rawValue).length > maxValueBytes || names.has(lower)) {
      throw new Error(text.invalidResolverHeader);
    }
    names.add(lower);
    output[canonicalHeaderName(name)] = rawValue;
  }
  return Object.keys(output).length ? output : undefined;
}

function parseGlob(value: unknown, label: string, text: Text["authConsole"]) {
  const pattern = stringValue(value, label, text);
  let bracket = false;
  let escaped = false;
  for (const character of pattern) {
    if (escaped) { escaped = false; continue; }
    if (character === "\\") { escaped = true; continue; }
    if (character === "[") bracket = true;
    if (character === "]" && bracket) bracket = false;
  }
  if (escaped || bracket) throw new Error(text.invalidGlob(label));
  return pattern;
}

function parseHeaderMutation(value: unknown, label: string, text: Text["authConsole"], requestOnly: boolean): DirectiveHeaderMutation {
  const input = record(value, label, text);
  knownKeys(input, ["side", "action", "name", "glob", "values"], label, text);
  if (input.side !== "request" && input.side !== "response") throw new Error(text.onlyValues(`${label}.side`, "request, response"));
  if (requestOnly && input.side !== "request") throw new Error(text.onlyValues(`${label}.side`, "request"));
  const side = input.side;
  if (input.action !== "set" && input.action !== "remove" && input.action !== "append") throw new Error(text.onlyValues(`${label}.action`, "set, remove, append"));
  const hasName = input.name !== undefined;
  const hasGlob = input.glob !== undefined;
  if (hasName === hasGlob) throw new Error(text.exactlyOneSelector(label));
  let selector: { name: string } | { glob: string };
  let exactName: string | undefined;
  if (hasName) {
    exactName = stringValue(input.name, `${label}.name`, text);
    if (!isHeaderName(exactName)) throw new Error(text.invalidHeaderName(`${label}.name`));
    if (side === "response" && (protectedResponseHeaders.has(exactName.toLowerCase()) || exactName.toLowerCase().startsWith("x-dproxy-"))) throw new Error(text.invalidHeaderName(`${label}.name`));
    selector = { name: exactName };
  } else {
    selector = { glob: parseGlob(input.glob, `${label}.glob`, text) };
  }
  if (input.action === "remove") {
    if (input.values !== undefined && arrayValue(input.values, `${label}.values`, text).length) throw new Error(text.removeHasValues(label));
    return { side, action: input.action, ...selector };
  }
  const values = arrayValue(input.values, `${label}.values`, text);
  if (!values.length || values.some((item) => typeof item !== "string" || !isHeaderValue(item))) throw new Error(text.setNeedsValues(label));
  if (side === "request" && exactName?.toLowerCase() === "host" && (input.action === "append" || values.length !== 1)) throw new Error(text.hostValues(label));
  return { side, action: input.action, ...selector, values: values as string[] };
}

function parseModule(value: unknown, label: string, text: Text["authConsole"]): ModuleSpec {
  const input = record(value, label, text);
  knownKeys(input, ["id", "module", "config"], label, text);
  if (typeof input.id !== "string" || input.id === "" || input.id !== input.id.trim()) throw new Error(text.nonEmptyString(`${label}.id`));
  if (typeof input.module !== "string" || input.module === "" || input.module !== input.module.trim()) throw new Error(text.nonEmptyString(`${label}.module`));
  const id = input.id;
  const module = input.module;
  const moduleName = /^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$/;
  if (!moduleName.test(id) || !moduleName.test(module) || new TextEncoder().encode(id).length > 64 || new TextEncoder().encode(module).length > 64) {
    throw new Error(text.mustBe(label, "module names using lowercase letters, digits, dots, or hyphens"));
  }
  if (input.config !== undefined && new TextEncoder().encode(JSON.stringify(input.config)).length > 65536) throw new Error(text.mustBe(`${label}.config`, "JSON <= 64 KiB"));
  return { id, module, ...(input.config === undefined ? {} : { config: input.config }) };
}

function parseProgram(value: unknown, label: string, text: Text["authConsole"], allowAttempt: boolean): DirectiveProgram | undefined {
  if (value === undefined) return undefined;
  const input = record(value, label, text);
  knownKeys(input, allowAttempt ? ["request", "attempt"] : ["request"], label, text);
  const parseList = (raw: unknown, listLabel: string) => {
    if (raw === undefined) return undefined;
    const values = arrayValue(raw, listLabel, text);
    if (values.length > 16) throw new Error(text.mustBe(listLabel, "array with at most 16 modules"));
    const modules = values.map((item, index) => parseModule(item, `${listLabel}[${index}]`, text));
    if (new Set(modules.map((item) => item.id)).size !== modules.length) throw new Error(text.mustBe(listLabel, "modules with unique ids"));
    return modules.length ? modules : undefined;
  };
  const request = parseList(input.request, `${label}.request`);
  const attempt = allowAttempt ? parseList(input.attempt, `${label}.attempt`) : undefined;
  return request || attempt ? { ...(request ? { request } : {}), ...(attempt ? { attempt } : {}) } : undefined;
}

function parsePayload(value: unknown, text: Text["authConsole"]): DirectivePayload {
  const input = record(value, "payload", text);
  knownKeys(input, ["target", "proxy", "headers", "program", "recovery"], "payload", text);
  const targetInput = record(input.target, "payload.target", text);
  knownKeys(targetInput, ["url", "join_path"], "payload.target", text);
  const target = {
    url: parseURL(targetInput.url, "payload.target.url", ["http", "https"], text),
    ...(targetInput.join_path === undefined ? {} : { join_path: booleanValue(targetInput.join_path, "payload.target.join_path", text) }),
  };
  const proxy = optionalString(input.proxy, "payload.proxy", text);
  if (proxy !== undefined) {
    const parsed = parseURL(proxy, "payload.proxy", ["socks5"], text);
    const url = new URL(parsed);
    const hasUserInfo = Boolean(url.username || url.password);
    if (!url.port || url.pathname !== "" || url.search || url.hash || hasUserInfo && (!url.username.trim() || !url.password)) throw new Error(text.mustBe("payload.proxy", "socks5 URL with host and port"));
  }
  let headers: DirectivePayload["headers"];
  if (input.headers !== undefined) {
    const headersInput = record(input.headers, "payload.headers", text);
    knownKeys(headersInput, ["mode", "preserve_proxy_disclosure", "mutations"], "payload.headers", text);
    if (headersInput.mode !== undefined && headersInput.mode !== "patch" && headersInput.mode !== "replace") throw new Error(text.onlyValues("payload.headers.mode", "patch, replace"));
    headers = {
      ...(headersInput.mode === undefined ? {} : { mode: headersInput.mode }),
      ...(headersInput.preserve_proxy_disclosure === undefined ? {} : { preserve_proxy_disclosure: booleanValue(headersInput.preserve_proxy_disclosure, "payload.headers.preserve_proxy_disclosure", text) }),
      ...(headersInput.mutations === undefined ? {} : { mutations: arrayValue(headersInput.mutations, "payload.headers.mutations", text).map((item, index) => parseHeaderMutation(item, `payload.headers.mutations[${index}]`, text, false)) }),
    };
  }
  const program = parseProgram(input.program, "payload.program", text, true);
  const recovery = parseRecovery(input.recovery, text);
  return { target, ...(proxy ? { proxy } : {}), ...(headers ? { headers } : {}), ...(program ? { program } : {}), ...(recovery ? { recovery } : {}) };
}

function durationMilliseconds(value: string) {
  if (value.startsWith("+")) value = value.slice(1);
  const matchPattern = /(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/gy;
  const factors: Record<string, number> = { ns: 1e-6, us: 1e-3, "µs": 1e-3, "μs": 1e-3, ms: 1, s: 1000, m: 60000, h: 3600000 };
  let position = 0;
  let total = 0;
  for (const match of value.matchAll(matchPattern)) {
    if (match.index !== position) return undefined;
    total += Number(match[1]) * factors[match[2]];
    position += match[0].length;
  }
  return position === value.length ? total : undefined;
}

function parseDuration(value: unknown, label: string, text: Text["authConsole"], fallback?: string) {
  const raw = optionalString(value, label, text) ?? fallback;
  if (raw === undefined) return undefined;
  const milliseconds = durationMilliseconds(raw);
  if (milliseconds === undefined || milliseconds <= 0 || milliseconds > 600000) throw new Error(text.mustBe(label, "positive Go duration <= 10m"));
  return raw;
}

function parseRecovery(value: unknown, text: Text["authConsole"]): RecoverySpec | undefined {
  if (value === undefined) return undefined;
  const input = record(value, "recovery", text);
  knownKeys(input, ["controller", "triggers", "budget"], "recovery", text);
  const controllerInput = record(input.controller, "recovery.controller", text);
  knownKeys(controllerInput, ["url", "headers", "timeout"], "recovery.controller", text);
  const controllerHeaders = parseHeaderMap(controllerInput.headers, "recovery.controller.headers", text, 64, 8192);
  const controller = {
    url: parseURL(controllerInput.url, "recovery.controller.url", ["http", "https"], text, false),
    ...(controllerHeaders ? { headers: controllerHeaders } : {}),
    timeout: parseDuration(controllerInput.timeout, "recovery.controller.timeout", text, "3s"),
  };
  const triggersInput = record(input.triggers, "recovery.triggers", text);
  knownKeys(triggersInput, ["response_header_timeout", "unexpected_status", "transport_error"], "recovery.triggers", text);
  const responseHeaderTimeout = parseDuration(triggersInput.response_header_timeout, "recovery.triggers.response_header_timeout", text);
  let unexpectedStatus: NonNullable<RecoverySpec["triggers"]["unexpected_status"]> | undefined;
  if (triggersInput.unexpected_status !== undefined) {
    const statusInput = record(triggersInput.unexpected_status, "recovery.triggers.unexpected_status", text);
    knownKeys(statusInput, ["expected", "capture_body_bytes"], "recovery.triggers.unexpected_status", text);
    const expected = arrayValue(statusInput.expected, "recovery.triggers.unexpected_status.expected", text).map((item, index) => {
      const range = record(item, `recovery.triggers.unexpected_status.expected[${index}]`, text);
      knownKeys(range, ["from", "to"], `recovery.triggers.unexpected_status.expected[${index}]`, text);
      return {
        from: integerValue(range.from, `recovery.triggers.unexpected_status.expected[${index}].from`, text, 200, 599),
        to: integerValue(range.to, `recovery.triggers.unexpected_status.expected[${index}].to`, text, 200, 599),
      };
    }).sort((left, right) => left.from - right.from || left.to - right.to);
    if (!expected.length || expected.some((item, index) => item.from > item.to || index > 0 && item.from <= expected[index - 1].to)) throw new Error(text.mustBe("recovery.triggers.unexpected_status.expected", "non-overlapping status ranges from 200 to 599"));
    unexpectedStatus = {
      expected,
      capture_body_bytes: statusInput.capture_body_bytes === undefined
        ? 65536
        : integerValue(statusInput.capture_body_bytes, "recovery.triggers.unexpected_status.capture_body_bytes", text, 1, 16 << 20),
    };
  }
  const transportError = triggersInput.transport_error === undefined ? false : booleanValue(triggersInput.transport_error, "recovery.triggers.transport_error", text);
  if (!responseHeaderTimeout && !unexpectedStatus && !transportError) throw new Error(text.mustBe("recovery.triggers", "object with at least one enabled trigger"));
  const budgetInput = record(input.budget, "recovery.budget", text);
  knownKeys(budgetInput, ["max_attempts", "max_elapsed"], "recovery.budget", text);
  return {
    controller,
    triggers: {
      ...(responseHeaderTimeout ? { response_header_timeout: responseHeaderTimeout } : {}),
      ...(unexpectedStatus ? { unexpected_status: unexpectedStatus } : {}),
      ...(transportError ? { transport_error: true } : {}),
    },
    budget: {
      max_attempts: integerValue(budgetInput.max_attempts, "recovery.budget.max_attempts", text, 1, 100),
      max_elapsed: parseDuration(budgetInput.max_elapsed, "recovery.budget.max_elapsed", text, "30s"),
    },
  };
}

function parseRemoteSpec(value: unknown, text: Text["authConsole"]): RemoteSpec {
  const input = record(value, "remote", text);
  knownKeys(input, ["http", "redis", "file"], "remote", text);
  const backends = ["http", "redis", "file"].filter((name) => input[name] !== undefined);
  if (backends.length !== 1) throw new Error(text.mustBe("remote", "object with exactly one of http, redis, file"));
  if (input.file !== undefined) {
    const file = record(input.file, "remote.file", text);
    knownKeys(file, ["path"], "remote.file", text);
    if (typeof file.path !== "string" || !isRemoteFilePathValid(file.path)) throw new Error(text.invalidFilePath);
    return { file: { path: file.path } };
  }
  if (input.redis !== undefined) {
    const redis = record(input.redis, "remote.redis", text);
    knownKeys(redis, ["url", "key"], "remote.redis", text);
    if (typeof redis.key !== "string" || !isRemoteKeyValid(redis.key)) throw new Error(text.invalidRedisKey);
    return { redis: { url: parseURL(redis.url, "remote.redis.url", ["redis", "rediss"], text, true, false), key: redis.key } };
  }
  const http = record(input.http, "remote.http", text);
  knownKeys(http, ["url", "headers"], "remote.http", text);
  let headers: Extract<RemoteSpec, { http: unknown }>["http"]["headers"];
  if (http.headers !== undefined) {
    const headerInput = record(http.headers, "remote.http.headers", text);
    knownKeys(headerInput, ["mode", "preserve_proxy_disclosure", "mutations"], "remote.http.headers", text);
    if (headerInput.mode !== undefined && headerInput.mode !== "patch" && headerInput.mode !== "replace") throw new Error(text.onlyValues("remote.http.headers.mode", "patch, replace"));
    headers = {
      ...(headerInput.mode === undefined ? {} : { mode: headerInput.mode }),
      ...(headerInput.preserve_proxy_disclosure === undefined ? {} : { preserve_proxy_disclosure: booleanValue(headerInput.preserve_proxy_disclosure, "remote.http.headers.preserve_proxy_disclosure", text) }),
      ...(headerInput.mutations === undefined ? {} : { mutations: arrayValue(headerInput.mutations, "remote.http.headers.mutations", text).map((item, index) => parseHeaderMutation(item, `remote.http.headers.mutations[${index}]`, text, true)) }),
    };
  }
  return {
    http: {
      url: parseURL(http.url, "remote.http.url", ["http", "https"], text, false, false),
      ...(headers ? { headers } : {}),
    },
  };
}

export function isRemoteFilePathValid(value: string) {
  const bytes = new TextEncoder().encode(value);
  if (!value || value === "." || value !== value.trim() || bytes.length > 4096 || value.includes("\\")) return false;
  const segments = value.split("/");
  return segments.every((segment) => Boolean(segment) && segment !== "." && segment !== ".." && ![...segment].some((character) => {
    const point = character.codePointAt(0) ?? 0;
    return point === 0 || point < 0x20 || point === 0x7f;
  }));
}

export function isRemoteKeyValid(value: string) {
  const bytes = new TextEncoder().encode(value);
  return Boolean(value) && value === value.trim() && bytes.length <= 256 && ![...value].some((character) => {
    const point = character.codePointAt(0) ?? 0;
    return point === 0 || point < 0x20 || point === 0x7f;
  });
}

export function parseTokenDocument(kind: TokenKind, value: unknown, text: Text["authConsole"]): DirectiveEnvelope {
  if (kind === "inline") {
    return { kind, document: parsePayload(value, text) };
  }
  return { kind, document: parseRemoteSpec(value, text) };
}
