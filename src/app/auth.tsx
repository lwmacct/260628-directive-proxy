import { GithubOutlined } from "@ant-design/icons";
import { Alert, Avatar, Button, Card, Flex, Spin, Typography } from "antd";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

export type AuthIdentity = {
  github_id: string;
  username: string;
  avatar_url: string;
};

type AuthContextValue = {
  identity: AuthIdentity;
  logout: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);
const authRequiredEvent = "dproxy:auth-required";

export async function apiFetch(input: RequestInfo | URL, init?: RequestInit) {
  const response = await fetch(input, init);
  if (response.status === 401) window.dispatchEvent(new Event(authRequiredEvent));
  return response;
}

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth must be used inside AuthGate");
  return value;
}

export function AuthGate({ children }: { children: ReactNode }) {
  const [identity, setIdentity] = useState<AuthIdentity | null>(null);
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState(false);

  const loadSession = useCallback(async () => {
    setLoading(true);
    setUnavailable(false);
    try {
      const response = await fetch("/auth/session");
      if (response.status === 401) {
        setIdentity(null);
        return;
      }
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      setIdentity((await response.json()) as AuthIdentity);
    } catch {
      setUnavailable(true);
      setIdentity(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => void loadSession(), [loadSession]);
  useEffect(() => {
    const reset = () => setIdentity(null);
    window.addEventListener(authRequiredEvent, reset);
    return () => window.removeEventListener(authRequiredEvent, reset);
  }, []);

  const logout = useCallback(async () => {
    const response = await fetch("/auth/logout", { method: "POST" });
    if (!response.ok && response.status !== 401) throw new Error(`HTTP ${response.status}`);
    setIdentity(null);
  }, []);

  const value = useMemo(() => identity && { identity, logout }, [identity, logout]);
  if (loading) return <Flex align="center" justify="center" style={{ minHeight: "100vh" }}><Spin size="large" /></Flex>;
  if (!value) return <LoginPage unavailable={unavailable} onRetry={loadSession} />;
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

function LoginPage({ unavailable, onRetry }: { unavailable: boolean; onRetry: () => void }) {
  return (
    <Flex align="center" justify="center" style={{ minHeight: "100vh", padding: 24 }}>
      <Card style={{ maxWidth: 420, width: "100%" }}>
        <Flex align="center" gap={20} vertical>
          <Avatar size={64}>D</Avatar>
          <Typography.Title level={3} style={{ margin: 0 }}>LLM Relay DProxy</Typography.Title>
          <Typography.Text type="secondary">使用获准的 GitHub 管理员账号登录控制台</Typography.Text>
          {unavailable && <Alert message="认证服务暂时不可用" showIcon type="error" />}
          <Button block href="/auth/login" icon={<GithubOutlined />} size="large" type="primary">使用 GitHub 登录</Button>
          {unavailable && <Button onClick={onRetry}>重新检查</Button>}
        </Flex>
      </Card>
    </Flex>
  );
}
