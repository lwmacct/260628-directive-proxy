import {
  WorkbenchLanguageToggle,
  WorkbenchShell,
  WorkbenchThemeToggle,
  WorkbenchUserMenu,
  type WorkbenchNavEntry,
} from "@lwmacct/260627-antd-workbench";
import { AppstoreOutlined, GithubOutlined, SettingOutlined } from "@ant-design/icons";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { ConsoleLayout } from "../modules/console/ConsoleLayout";
import { SettingsLayout } from "../modules/settings/SettingsLayout";
import { SettingsPage } from "../modules/settings/SettingsPage";
import { DISPLAY_VERSION } from "../shared/appConfig";
import { useText } from "../shared/i18n";
import { useAuth } from "./auth";

function activeNav(pathname: string) {
  if (pathname.startsWith("/settings")) {
    return "settings";
  }
  return "console";
}

export function AppShell() {
  const location = useLocation();
  const navigate = useNavigate();
  const t = useText();
  const { identity, logout } = useAuth();
  const nav: WorkbenchNavEntry[] = [
    { key: "console", label: t.app.console, icon: <AppstoreOutlined /> },
    { key: "settings", label: t.app.settings, icon: <SettingOutlined /> },
  ];

  return (
    <WorkbenchShell
      account={
        <WorkbenchUserMenu
          user={{
            avatarUrl: identity.avatar_url,
            displayName: identity.name,
            provider: "GitHub",
            providerIcon: <GithubOutlined />,
            username: identity.username,
          }}
          onLogout={logout}
        />
      }
      utilities={
        <>
          <WorkbenchThemeToggle />
          <WorkbenchLanguageToggle />
        </>
      }
      brand={{
        mark: "D",
        name: "LLM Relay DProxy",
        version: DISPLAY_VERSION,
      }}
      flushContent
      nav={nav}
      selectedNavKey={activeNav(location.pathname)}
      onSelectNav={(key) => navigate(key === "settings" ? "/settings" : "/console")}
    >
      <Routes>
        <Route element={<Navigate replace to="/console" />} path="/" />
        <Route element={<ConsoleLayout />} path="/console/*" />
        <Route element={<SettingsLayout />} path="/settings">
          <Route element={<Navigate replace to="/settings/appearance" />} index />
          <Route element={<SettingsPage />} path="appearance" />
        </Route>
        <Route element={<Navigate replace to="/console" />} path="*" />
      </Routes>
    </WorkbenchShell>
  );
}
