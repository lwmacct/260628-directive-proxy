import {
  WorkbenchLanguageToggle,
  WorkbenchShell,
  WorkbenchThemeToggle,
  useWorkbenchLocale,
  type WorkbenchNavEntry,
} from "@lwmacct/260627-antd-workbench";
import { AppstoreOutlined, LogoutOutlined, SettingOutlined } from "@ant-design/icons";
import { Avatar, Button, Space, Typography } from "antd";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { ConsoleLayout } from "../modules/console/ConsoleLayout";
import { SettingsLayout } from "../modules/settings/SettingsLayout";
import { SettingsPage } from "../modules/settings/SettingsPage";
import { useAuth } from "./auth";

const nav: WorkbenchNavEntry[] = [
  { key: "console", label: "控制台", icon: <AppstoreOutlined /> },
  { key: "settings", label: "设置", icon: <SettingOutlined /> },
];

function activeNav(pathname: string) {
  if (pathname.startsWith("/settings")) {
    return "settings";
  }
  return "console";
}

export function AppShell() {
  const location = useLocation();
  const navigate = useNavigate();
  const { locale } = useWorkbenchLocale();
  const { identity, logout } = useAuth();

  return (
    <WorkbenchShell
      actions={
        <Space>
          <Avatar size="small" src={identity.avatar_url} />
          <Typography.Text>{identity.username}</Typography.Text>
          <WorkbenchThemeToggle />
          <WorkbenchLanguageToggle
            labels={{ switchLanguage: locale.startsWith("zh") ? "切换语言" : "Switch language" }}
          />
          <Button icon={<LogoutOutlined />} type="text" onClick={() => void logout()}>退出</Button>
        </Space>
      }
      brand={{
        mark: "D",
        name: "LLM Relay DProxy",
        subtitle: "Directive proxy control plane",
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
