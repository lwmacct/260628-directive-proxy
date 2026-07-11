import {
  WorkbenchOAuthSignInPage,
} from "@lwmacct/260627-antd-workbench";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useText } from "../shared/i18n";
import { loadSession, type AuthIdentity, type SessionState } from "./session";

type AuthState =
  | SessionState
  | { status: "signing-in"; provider: "github" };

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

export function AuthBoundary({ children, initialSession }: { children: ReactNode; initialSession: SessionState }) {
  const t = useText();
  const [state, setState] = useState<AuthState>(initialSession);

  const retrySession = useCallback(async () => {
    setState(await loadSession());
  }, []);

  useEffect(() => {
    const update = (event: Event) => {
      const status = (event as CustomEvent<"signed-out" | "forbidden">).detail;
      setState({ status });
    };
    window.addEventListener(authStateEvent, update);
    return () => window.removeEventListener(authStateEvent, update);
  }, []);

  const logout = useCallback(async () => {
    const response = await fetch("/oidcauth/logout", { method: "POST" });
    if (!response.ok && response.status !== 401) throw new Error(`HTTP ${response.status}`);
    setState({ status: "signed-out" });
  }, []);

  const login = useCallback(() => {
    setState({ status: "signing-in", provider: "github" });
    const returnTo = window.location.pathname + window.location.search + window.location.hash;
    window.requestAnimationFrame(() => {
      window.location.assign(`/oidcauth/login?return_to=${encodeURIComponent(returnTo)}`);
    });
  }, []);

  const value = useMemo(
    () => state.status === "authenticated" ? { identity: state.identity, logout } : null,
    [logout, state],
  );

  if (!value) {
    return (
      <WorkbenchOAuthSignInPage
        brand={{
          description: t.auth.signInDescription,
          mark: "D",
          name: "LLM Relay DProxy",
        }}
        hint={state.status === "signed-out" ? t.auth.authorizedOnly : undefined}
        error={state.status === "unavailable" ? t.auth.unavailable : state.status === "forbidden" ? t.auth.forbidden : undefined}
        pendingProvider={state.status === "signing-in" ? state.provider : undefined}
        providers={[{ label: "GitHub", provider: "github" }]}
        retry={state.status === "unavailable"}
        onRetry={state.status === "unavailable" ? retrySession : undefined}
        onSelectProvider={login}
      />
    );
  }
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

function dispatchAuthState(status: "signed-out" | "forbidden") {
  window.dispatchEvent(new CustomEvent(authStateEvent, { detail: status }));
}
