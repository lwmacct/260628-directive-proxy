import type { Text } from "../../shared/i18n";
import { parseTokenDocument } from "./schema";
import type { DirectiveEnvelope, TokenKind } from "./types";

export const TOKEN_FAMILY = "dp";
export const TOKEN_VERSION = "22";

export function normalizeDirectiveToken(value: string) {
	return value.trim().replace(/^Bearer[ \t]+/i, "");
}

function encodeBase64URL(value: string) {
	const bytes = new TextEncoder().encode(value);
	return encodeBase64URLBytes(bytes);
}

function encodeBase64URLBytes(bytes: Uint8Array) {
	let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

function decodeBase64URLBytes(value: string, text: Text["directiveConsole"]) {
	if (!/^[A-Za-z0-9_-]+$/.test(value)) throw new Error(text.tokenDecodeFailed);
	const padded = value.replaceAll("-", "+").replaceAll("_", "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
	try {
		return Uint8Array.from(atob(padded), (character) => character.charCodeAt(0));
	} catch {
		throw new Error(text.tokenDecodeFailed);
	}
}

async function importHMACKey(secret: string, text: Text["directiveConsole"]) {
	if (!secret.trim()) throw new Error(text.tokenSecretRequired);
	return crypto.subtle.importKey("raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign", "verify"]);
}

async function signToken(secret: string, value: string, text: Text["directiveConsole"]) {
	const key = await importHMACKey(secret, text);
	const signature = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(value));
	return encodeBase64URLBytes(new Uint8Array(signature));
}

function decodeBase64URL(value: string, text: Text["directiveConsole"]) {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) throw new Error(text.tokenDecodeFailed);
  const padded = value.replaceAll("-", "+").replaceAll("_", "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
  let binary: string;
  try { binary = atob(padded); } catch { throw new Error(text.tokenDecodeFailed); }
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  try { return new TextDecoder("utf-8", { fatal: true }).decode(bytes); } catch { throw new Error(text.tokenDecodeFailed); }
}

export async function encodeDirective(envelope: DirectiveEnvelope, secret: string, text: Text["directiveConsole"]) {
	const payload = encodeBase64URL(JSON.stringify(envelope.document));
	const signingInput = `${TOKEN_FAMILY}.${TOKEN_VERSION}.${envelope.kind}.${payload}`;
	return `${signingInput}.${await signToken(secret, payload, text)}`;
}

export async function decodeDirective(value: string, secret: string, text: Text["directiveConsole"]): Promise<DirectiveEnvelope> {
	const parts = normalizeDirectiveToken(value).split(".");
	if (parts.length !== 5 || parts[0] !== TOKEN_FAMILY || parts[1] !== TOKEN_VERSION || parts[2] !== "inline" && parts[2] !== "remote" || !parts[3] || !parts[4]) {
		throw new Error(text.tokenPrefix);
	}
	const key = await importHMACKey(secret, text);
	const signature = decodeBase64URLBytes(parts[4], text);
	if (!await crypto.subtle.verify("HMAC", key, signature, new TextEncoder().encode(parts[3]))) {
		throw new Error(text.tokenAuthenticationFailed);
	}
	let parsed: unknown;
	try { parsed = JSON.parse(decodeBase64URL(parts[3], text)); } catch (error) {
    if (error instanceof SyntaxError) throw new Error(text.tokenDecodeFailed);
    throw error;
  }
  return parseTokenDocument(parts[2] as TokenKind, parsed, text);
}

export function parseDirectiveJSON(kind: TokenKind, value: string, text: Text["directiveConsole"]) {
  let parsed: unknown;
  try { parsed = JSON.parse(value); } catch { throw new Error(text.jsonParseFailed); }
  return parseTokenDocument(kind, parsed, text);
}

export function validateDirective(envelope: DirectiveEnvelope, text: Text["directiveConsole"]) {
  return parseTokenDocument(envelope.kind, envelope.document, text);
}

export function formatDirectiveJSON(envelope: DirectiveEnvelope) {
  return JSON.stringify(envelope.document, null, 2);
}
