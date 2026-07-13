export type AuthMode = "oidc" | "token";

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
  | { status: "authenticated"; access: "denied" | "granted"; identity: AuthIdentity; mode: AuthMode }
  | { status: "signed-out"; mode: AuthMode }
  | { status: "unavailable"; mode?: AuthMode };

type SessionResponse =
  | { status: "authenticated"; access: "denied" | "granted"; identity: AuthIdentity }
  | { status: "signed-out" };

export async function loadSession(): Promise<SessionState> {
  let mode: AuthMode | undefined;
  try {
    const configResponse = await fetch("/auth/config");
    if (!configResponse.ok) return { status: "unavailable" };
    const config: unknown = await configResponse.json();
    mode = parseAuthMode(config);
    if (!mode) return { status: "unavailable" };

    const response = await fetch(authEndpoint(mode, "session"));
    if (!response.ok) return { status: "unavailable", mode };
    const session: unknown = await response.json();
    return isSessionResponse(session) ? { ...session, mode } : { status: "unavailable", mode };
  } catch {
    return { status: "unavailable", mode };
  }
}

export function authEndpoint(mode: AuthMode, action: "login" | "logout" | "session") {
  return `/${mode === "oidc" ? "oidcauth" : "tokenauth"}/${action}`;
}

function parseAuthMode(value: unknown): AuthMode | undefined {
  if (!value || typeof value !== "object" || !("mode" in value)) return undefined;
  return value.mode === "oidc" || value.mode === "token" ? value.mode : undefined;
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
