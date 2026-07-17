import type { Text } from "../../shared/i18n";
import { parseTokenDocument } from "./schema";
import type { DirectiveEnvelope, TokenKind } from "./types";

export const TOKEN_FAMILY = "dp";
export const TOKEN_VERSION = "18";

function encodeBase64URL(value: string) {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

function decodeBase64URL(value: string, text: Text["authConsole"]) {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) throw new Error(text.tokenDecodeFailed);
  const padded = value.replaceAll("-", "+").replaceAll("_", "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
  let binary: string;
  try { binary = atob(padded); } catch { throw new Error(text.tokenDecodeFailed); }
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  try { return new TextDecoder("utf-8", { fatal: true }).decode(bytes); } catch { throw new Error(text.tokenDecodeFailed); }
}

export function encodeDirective(envelope: DirectiveEnvelope) {
  return `${TOKEN_FAMILY}.${TOKEN_VERSION}.${envelope.kind}.${encodeBase64URL(JSON.stringify(envelope.document))}`;
}

export function decodeDirective(value: string, text: Text["authConsole"]): DirectiveEnvelope {
  const parts = value.trim().split(".");
  if (parts.length !== 4 || parts[0] !== TOKEN_FAMILY || parts[1] !== TOKEN_VERSION || parts[2] !== "inline" && parts[2] !== "remote" || !parts[3]) {
    throw new Error(text.tokenPrefix);
  }
  let parsed: unknown;
  try { parsed = JSON.parse(decodeBase64URL(parts[3], text)); } catch (error) {
    if (error instanceof SyntaxError) throw new Error(text.tokenDecodeFailed);
    throw error;
  }
  return parseTokenDocument(parts[2] as TokenKind, parsed, text);
}

export function parseDirectiveJSON(kind: TokenKind, value: string, text: Text["authConsole"]) {
  let parsed: unknown;
  try { parsed = JSON.parse(value); } catch { throw new Error(text.jsonParseFailed); }
  return parseTokenDocument(kind, parsed, text);
}

export function validateDirective(envelope: DirectiveEnvelope, text: Text["authConsole"]) {
  return parseTokenDocument(envelope.kind, envelope.document, text);
}

export function formatDirectiveJSON(envelope: DirectiveEnvelope) {
  return JSON.stringify(envelope.document, null, 2);
}
