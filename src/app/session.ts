import { authmeEndpoint } from "./authme";

export type AuthMethodID = "github" | "token";

export type AuthMethod = {
  id: AuthMethodID;
  flow: "redirect" | "secret";
  label: string;
};

export type AuthIdentity = {
  subject: string;
  username: string;
  name?: string;
  email?: string;
  avatar_url?: string;
  provider?: string;
  provider_user_id?: string;
};

export type SessionState =
  | { status: "authenticated"; access: "denied" | "granted"; identity: AuthIdentity; method: AuthMethodID; methods: AuthMethod[] }
  | { status: "signed-out"; methods: AuthMethod[] }
  | { status: "unavailable"; methods?: AuthMethod[] };

export async function loadSession(): Promise<SessionState> {
  try {
    const response = await fetch(authmeEndpoint("/session"));
    if (!response.ok) return { status: "unavailable" };
    const session: unknown = await response.json();
    return isSessionState(session) ? session : { status: "unavailable" };
  } catch {
    return { status: "unavailable" };
  }
}

function isSessionState(value: unknown): value is Exclude<SessionState, { status: "unavailable" }> {
  if (!value || typeof value !== "object" || !("status" in value) || !("methods" in value) || !isMethods(value.methods)) return false;
  if (value.status === "signed-out") return true;
  if (value.status !== "authenticated" || !("method" in value) || !isMethodID(value.method)
    || !("access" in value) || (value.access !== "granted" && value.access !== "denied") || !("identity" in value)) return false;
  const identity = value.identity;
  return Boolean(identity && typeof identity === "object" && "subject" in identity && typeof identity.subject === "string"
    && "username" in identity && typeof identity.username === "string");
}

function isMethods(value: unknown): value is AuthMethod[] {
  return Array.isArray(value) && value.length > 0 && value.every((method) => Boolean(
    method && typeof method === "object" && "id" in method && isMethodID(method.id)
    && "flow" in method && (method.flow === "redirect" || method.flow === "secret")
    && "label" in method && typeof method.label === "string",
  ));
}

function isMethodID(value: unknown): value is AuthMethodID {
  return value === "github" || value === "token";
}
