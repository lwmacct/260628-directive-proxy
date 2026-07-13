import {
  WorkbenchAccessDeniedPage,
  WorkbenchOAuthSignInPage,
  WorkbenchSecurityPage,
} from "@lwmacct/260627-antd-workbench";
import { GithubOutlined, KeyOutlined, LoginOutlined } from "@ant-design/icons";
import { Alert, Button, Form, Input, Space, Typography } from "antd";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useText } from "../shared/i18n";
import { authEndpoint, loadSession, type AuthIdentity, type AuthMode, type SessionState } from "./session";

type AuthState =
  | SessionState
  | { status: "signing-in"; mode: AuthMode }
  | { status: "invalid-token"; mode: "token" };

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
    if (!("mode" in state) || !state.mode) return;
    const mode = state.mode;
    setLogoutLoading(true);
    try {
      const response = await fetch(authEndpoint(mode, "logout"), { method: "POST" });
      setState(response.ok ? { status: "signed-out", mode } : { status: "unavailable", mode });
    } catch {
      setState({ status: "unavailable", mode });
    } finally {
      setLogoutLoading(false);
    }
  }, [state]);

  const oidcLogin = useCallback(() => {
    setState({ status: "signing-in", mode: "oidc" });
    const returnTo = window.location.pathname + window.location.search + window.location.hash;
    window.requestAnimationFrame(() => {
      window.location.assign(`${authEndpoint("oidc", "login")}?return_to=${encodeURIComponent(returnTo)}`);
    });
  }, []);

  const tokenLogin = useCallback(async (token: string) => {
    setState({ status: "signing-in", mode: "token" });
    try {
      const response = await fetch(authEndpoint("token", "login"), {
        body: JSON.stringify({ token }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      });
      if (!response.ok) {
        setState({ status: response.status === 401 ? "invalid-token" : "unavailable", mode: "token" });
        return;
      }
      await retrySession();
    } catch {
      setState({ status: "unavailable", mode: "token" });
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
    const mode = "mode" in state ? state.mode : undefined;
    if (mode === "token") {
      return (
        <AccessTokenSignInPage
          error={state.status === "unavailable" ? t.auth.unavailable : state.status === "invalid-token" ? t.auth.invalidToken : undefined}
          loading={state.status === "signing-in"}
          onRetry={state.status === "unavailable" ? retrySession : undefined}
          onSubmit={tokenLogin}
        />
      );
    }
    return (
      <WorkbenchOAuthSignInPage
        brand={{ description: t.auth.signInDescription, mark: "D", name: "Directive Proxy" }}
        hint={state.status === "signed-out" ? t.auth.authorizedOnly : undefined}
        error={state.status === "unavailable" ? t.auth.unavailable : undefined}
        pendingProvider={state.status === "signing-in" ? "github" : undefined}
        providers={[{ label: "GitHub", provider: "github" }]}
        retry={state.status === "unavailable"}
        onRetry={state.status === "unavailable" ? retrySession : undefined}
        onSelectProvider={oidcLogin}
      />
    );
  }
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

function AccessTokenSignInPage({
  error,
  loading,
  onRetry,
  onSubmit,
}: {
  error?: ReactNode;
  loading: boolean;
  onRetry?: () => void;
  onSubmit: (token: string) => Promise<void>;
}) {
  const t = useText();
  return (
    <WorkbenchSecurityPage brand={{ description: t.auth.signInDescription, mark: "D", name: "Directive Proxy" }}>
      <div className="wb-security-form">
        <Space className="wb-security__header" orientation="vertical" size={4}>
          <Typography.Title level={1}>{t.auth.accessTokenTitle}</Typography.Title>
          <Typography.Text type="secondary">{t.auth.accessTokenDescription}</Typography.Text>
        </Space>
        {error ? <Alert className="wb-security__alert" message={error} showIcon type="error" /> : null}
        <Form layout="vertical" requiredMark={false} onFinish={(values: { token: string }) => void onSubmit(values.token)}>
          <Form.Item label={t.auth.accessToken} name="token" rules={[{ required: true, message: t.auth.accessTokenRequired }]}>
            <Input.Password autoComplete="current-password" disabled={loading} prefix={<KeyOutlined />} />
          </Form.Item>
          <Button block htmlType="submit" icon={<LoginOutlined />} loading={loading} size="large" type="primary">
            {t.auth.signIn}
          </Button>
          {onRetry ? (
            <Button block type="text" onClick={onRetry}>{t.auth.retry}</Button>
          ) : null}
        </Form>
      </div>
    </WorkbenchSecurityPage>
  );
}

function dispatchAuthRefresh() {
  window.dispatchEvent(new Event(authStateEvent));
}
