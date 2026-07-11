import {
  WorkbenchAuthCheckingPage,
  WorkbenchOAuthSignInPage,
} from "@lwmacct/260627-antd-workbench";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useText } from "../shared/i18n";

export type AuthIdentity = {
  subject: string;
  username: string;
  name?: string;
  email?: string;
  avatar_url?: string;
  provider?: string;
  provider_user_id?: string;
};

type AuthState =
  | { status: "checking" }
  | { status: "authenticated"; identity: AuthIdentity }
  | { status: "signed-out" }
  | { status: "forbidden" }
  | { status: "unavailable" };

type AuthContextValue = {
  identity: AuthIdentity;
  logout: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);
const authStateEvent = "dproxy:auth-state";

export async function apiFetch(input: RequestInfo | URL, init?: RequestInit) {
  const response = await fetch(input, init);
  if (response.status === 401) dispatchAuthState("signed-out");
  if (response.status === 403) dispatchAuthState("forbidden");
  return response;
}

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth must be used inside AuthBoundary");
  return value;
}

export function AuthBoundary({ children }: { children: ReactNode }) {
  const t = useText();
  const [state, setState] = useState<AuthState>({ status: "checking" });

  const loadSession = useCallback(async () => {
    setState({ status: "checking" });
    try {
      const response = await fetch("/auth/session");
      if (response.status === 401) return setState({ status: "signed-out" });
      if (response.status === 403) return setState({ status: "forbidden" });
      if (!response.ok) return setState({ status: "unavailable" });
      setState({ status: "authenticated", identity: (await response.json()) as AuthIdentity });
    } catch {
      setState({ status: "unavailable" });
    }
  }, []);

  useEffect(() => void loadSession(), [loadSession]);
  useEffect(() => {
    const update = (event: Event) => {
      const status = (event as CustomEvent<"signed-out" | "forbidden">).detail;
      setState({ status });
    };
    window.addEventListener(authStateEvent, update);
    return () => window.removeEventListener(authStateEvent, update);
  }, []);

  const logout = useCallback(async () => {
    const response = await fetch("/auth/logout", { method: "POST" });
    if (!response.ok && response.status !== 401) throw new Error(`HTTP ${response.status}`);
    setState({ status: "signed-out" });
  }, []);

  const value = useMemo(
    () => state.status === "authenticated" ? { identity: state.identity, logout } : null,
    [logout, state],
  );

  if (state.status === "checking") return <WorkbenchAuthCheckingPage label={t.auth.checking} />;
  if (!value) {
    return (
      <WorkbenchOAuthSignInPage
        brand={{
          description: state.status === "forbidden"
            ? t.auth.forbiddenDescription
            : t.auth.signInDescription,
          mark: "D",
          name: "LLM Relay DProxy",
        }}
        error={state.status === "unavailable" ? t.auth.unavailable : state.status === "forbidden" ? t.auth.forbidden : undefined}
        providers={[{ label: "GitHub", provider: "github" }]}
        onRetry={state.status === "unavailable" ? loadSession : undefined}
        onSelectProvider={() => window.location.assign("/auth/login")}
      />
    );
  }
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

function dispatchAuthState(status: "signed-out" | "forbidden") {
  window.dispatchEvent(new CustomEvent(authStateEvent, { detail: status }));
}
