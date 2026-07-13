export type AuthMethod = "oidc" | "token";

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
  | { status: "authenticated"; access: "denied" | "granted"; identity: AuthIdentity; method: AuthMethod; methods: AuthMethod[] }
  | { status: "signed-out"; methods: AuthMethod[] }
  | { status: "unavailable"; methods?: AuthMethod[] };

type SessionResponse =
  | { status: "authenticated"; access: "denied" | "granted"; identity: AuthIdentity }
  | { status: "signed-out" };

export async function loadSession(): Promise<SessionState> {
  let methods: AuthMethod[] | undefined;
  try {
    const configResponse = await fetch("/auth/config");
    if (!configResponse.ok) return { status: "unavailable" };
    const config: unknown = await configResponse.json();
    methods = parseAuthMethods(config);
    if (!methods) return { status: "unavailable" };

    const sessions = await Promise.all(methods.map(async (method) => ({
      method,
      session: await loadMethodSession(method),
    })));
    const granted = sessions.find((item) => item.session?.status === "authenticated" && item.session.access === "granted");
    if (granted?.session?.status === "authenticated") {
      return { ...granted.session, method: granted.method, methods };
    }
    const denied = sessions.find((item) => item.session?.status === "authenticated");
    const alternateSignedOut = sessions.some((item) => item.session?.status === "signed-out");
    if (denied?.session?.status === "authenticated" && !alternateSignedOut) {
      return { ...denied.session, method: denied.method, methods };
    }
    return sessions.every((item) => item.session?.status === "signed-out" || item.session?.status === "authenticated")
      ? { status: "signed-out", methods }
      : { status: "unavailable", methods };
  } catch {
    return { status: "unavailable", methods };
  }
}

export function authEndpoint(method: AuthMethod, action: "login" | "logout" | "session") {
  return `/${method === "oidc" ? "oidcauth" : "tokenauth"}/${action}`;
}

async function loadMethodSession(method: AuthMethod): Promise<SessionResponse | undefined> {
  const response = await fetch(authEndpoint(method, "session"));
  if (!response.ok) return undefined;
  const session: unknown = await response.json();
  return isSessionResponse(session) ? session : undefined;
}

function parseAuthMethods(value: unknown): AuthMethod[] | undefined {
  if (!value || typeof value !== "object" || !("methods" in value) || !Array.isArray(value.methods)) return undefined;
  const methods = value.methods.filter((method): method is AuthMethod => method === "oidc" || method === "token");
  return methods.length === value.methods.length && methods.length > 0 && new Set(methods).size === methods.length
    ? methods
    : undefined;
}

function isSessionResponse(value: unknown): value is SessionResponse {
  if (!value || typeof value !== "object" || !("status" in value)) return false;
  if (value.status === "signed-out") return true;
  if (value.status !== "authenticated" || !("access" in value) || (value.access !== "granted" && value.access !== "denied") || !("identity" in value)) return false;
  const identity = value.identity;
  return Boolean(
    identity
    && typeof identity === "object"
    && "subject" in identity
    && typeof identity.subject === "string"
    && "username" in identity
    && typeof identity.username === "string",
  );
}
