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
  | { status: "authenticated"; identity: AuthIdentity }
  | { status: "signed-out" }
  | { status: "forbidden" }
  | { status: "unavailable" };

export async function loadSession(): Promise<SessionState> {
  try {
    const response = await fetch("/oidcauth/session");
    if (response.status === 401) return { status: "signed-out" };
    if (response.status === 403) return { status: "forbidden" };
    if (!response.ok) return { status: "unavailable" };
    return { status: "authenticated", identity: (await response.json()) as AuthIdentity };
  } catch {
    return { status: "unavailable" };
  }
}
