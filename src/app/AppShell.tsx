import {
  WorkbenchAppearanceButton,
  WorkbenchLanguageToggle,
  WorkbenchShell,
  WorkbenchUserMenu,
} from "@lwmacct/260627-antd-workbench";
import { GithubOutlined, KeyOutlined } from "@ant-design/icons";
import { Navigate, Route, Routes } from "react-router-dom";
import { ConsoleLayout } from "../modules/console/ConsoleLayout";
import { DISPLAY_VERSION } from "../shared/appConfig";
import { useAuth } from "./auth";

export function AppShell() {
  const { identity, logout } = useAuth();

  return (
    <WorkbenchShell
      account={
        <WorkbenchUserMenu
          user={{
            avatarUrl: identity.avatar_url,
            displayName: identity.name,
            provider: identity.provider === "github" ? "GitHub" : "Access token",
            providerIcon: identity.provider === "github" ? <GithubOutlined /> : <KeyOutlined />,
            username: identity.username,
          }}
          onLogout={logout}
        />
      }
      brand={{
        mark: "D",
        name: "Directive Proxy",
        version: DISPLAY_VERSION,
      }}
      className="single-route-shell"
      flushContent
      nav={[]}
      utilities={
        <>
          <WorkbenchAppearanceButton />
          <WorkbenchLanguageToggle />
        </>
      }
      onSelectNav={() => undefined}
    >
      <Routes>
        <Route element={<Navigate replace to="/console" />} path="/" />
        <Route element={<ConsoleLayout />} path="/console/*" />
        <Route element={<Navigate replace to="/console" />} path="*" />
      </Routes>
    </WorkbenchShell>
  );
}
