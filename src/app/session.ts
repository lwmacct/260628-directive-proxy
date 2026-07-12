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
  | { status: "authenticated"; access: "denied" | "granted"; identity: AuthIdentity }
  | { status: "signed-out" }
  | { status: "unavailable" };

type SessionResponse = Exclude<SessionState, { status: "unavailable" }>;

export async function loadSession(): Promise<SessionState> {
  try {
    const response = await fetch("/oidcauth/session");
    if (!response.ok) return { status: "unavailable" };
    const session: unknown = await response.json();
    return isSessionResponse(session) ? session : { status: "unavailable" };
  } catch {
    return { status: "unavailable" };
  }
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
