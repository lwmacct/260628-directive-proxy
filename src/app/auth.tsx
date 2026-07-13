import {
  WorkbenchAccessDeniedPage,
  WorkbenchOAuthSignInPage,
  WorkbenchTokenSignInPage,
} from "@lwmacct/260627-antd-workbench";
import { GithubOutlined, KeyOutlined } from "@ant-design/icons";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useText } from "../shared/i18n";
import { authEndpoint, loadSession, type AuthIdentity, type AuthMethod, type SessionState } from "./session";

type AuthState =
  | SessionState
  | { status: "signing-in"; method: AuthMethod; methods: AuthMethod[] }
  | { status: "invalid-token"; methods: AuthMethod[] };

type AuthContextValue = {
  identity: AuthIdentity;
  logout: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);
const authStateEvent = "dproxy:auth-state";

export async function apiFetch(input: RequestInfo | URL, init?: RequestInit) {
  const response = await fetch(input, init);
  if (response.status === 401 || response.status === 403) dispatchAuthRefresh();
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
  const [logoutLoading, setLogoutLoading] = useState(false);

  const retrySession = useCallback(async () => {
    setState(await loadSession());
  }, []);

  useEffect(() => {
    const update = () => void retrySession();
    window.addEventListener(authStateEvent, update);
    return () => window.removeEventListener(authStateEvent, update);
  }, [retrySession]);

  const logout = useCallback(async () => {
    const methods = "methods" in state ? state.methods : undefined;
    if (!methods) return;
    setLogoutLoading(true);
    try {
      const responses = await Promise.all(methods.map((method) => fetch(authEndpoint(method, "logout"), { method: "POST" })));
      setState(responses.every((response) => response.ok)
        ? { status: "signed-out", methods }
        : { status: "unavailable", methods });
    } catch {
      setState({ status: "unavailable", methods });
    } finally {
      setLogoutLoading(false);
    }
  }, [state]);

  const oidcLogin = useCallback((methods: AuthMethod[]) => {
    setState({ status: "signing-in", method: "oidc", methods });
    const returnTo = window.location.pathname + window.location.search + window.location.hash;
    window.requestAnimationFrame(() => {
      window.location.assign(`${authEndpoint("oidc", "login")}?return_to=${encodeURIComponent(returnTo)}`);
    });
  }, []);

  const tokenLogin = useCallback(async (token: string, methods: AuthMethod[]) => {
    setState({ status: "signing-in", method: "token", methods });
    try {
      const response = await fetch(authEndpoint("token", "login"), {
        body: JSON.stringify({ token }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      });
      if (!response.ok) {
        setState(response.status === 401
          ? { status: "invalid-token", methods }
          : { status: "unavailable", methods });
        return;
      }
      await retrySession();
    } catch {
      setState({ status: "unavailable", methods });
    }
  }, [retrySession]);

  const value = useMemo(
    () => state.status === "authenticated" && state.access === "granted" ? { identity: state.identity, logout } : null,
    [logout, state],
  );

  if (state.status === "authenticated" && state.access === "denied") {
    return (
      <WorkbenchAccessDeniedPage
        brand={{ mark: "D", name: "Directive Proxy" }}
        identity={{
          avatarUrl: state.identity.avatar_url,
          displayName: state.identity.name,
          provider: state.identity.provider,
          providerIcon: state.identity.provider === "github" ? <GithubOutlined /> : <KeyOutlined />,
          username: state.identity.username,
        }}
        logoutLoading={logoutLoading}
        onLogout={() => void logout()}
      />
    );
  }

  if (!value) {
    const methods = "methods" in state ? state.methods : undefined;
    const tokenEnabled = methods?.includes("token") ?? false;
    const oidcEnabled = methods?.includes("oidc") ?? false;
    if (methods && tokenEnabled) {
      return (
        <WorkbenchTokenSignInPage
          brand={{ description: t.auth.signInDescription, mark: "D", name: "Directive Proxy" }}
          error={state.status === "unavailable" ? t.auth.unavailable : state.status === "invalid-token" ? t.auth.invalidToken : undefined}
          loading={state.status === "signing-in" && state.method === "token"}
          oauth={oidcEnabled ? {
            pendingProvider: state.status === "signing-in" && state.method === "oidc" ? "github" : undefined,
            providers: [{ label: "GitHub", provider: "github" }],
            onSelectProvider: () => oidcLogin(methods),
          } : undefined}
          retry={state.status === "unavailable"}
          onRetry={state.status === "unavailable" ? retrySession : undefined}
          onSubmit={({ token }) => tokenLogin(token, methods)}
        />
      );
    }
    return (
      <WorkbenchOAuthSignInPage
        brand={{ description: t.auth.signInDescription, mark: "D", name: "Directive Proxy" }}
        hint={state.status === "signed-out" ? t.auth.authorizedOnly : undefined}
        error={state.status === "unavailable" ? t.auth.unavailable : undefined}
        pendingProvider={state.status === "signing-in" && state.method === "oidc" ? "github" : undefined}
        providers={[{ disabled: !oidcEnabled, label: "GitHub", provider: "github" }]}
        retry={state.status === "unavailable"}
        onRetry={state.status === "unavailable" ? retrySession : undefined}
        onSelectProvider={() => methods && oidcLogin(methods)}
      />
    );
  }
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

function dispatchAuthRefresh() {
  window.dispatchEvent(new Event(authStateEvent));
}
