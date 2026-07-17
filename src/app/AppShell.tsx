import {
  WorkbenchAppearanceSettings,
  WorkbenchLanguageToggle,
  WorkbenchShell,
  WorkbenchThemeToggle,
  WorkbenchUserMenu,
} from "@lwmacct/260627-antd-workbench";
import { BgColorsOutlined, GithubOutlined, KeyOutlined } from "@ant-design/icons";
import { Button, Drawer, Flex, Tooltip } from "antd";
import { useState } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { ConsoleLayout } from "../modules/console/ConsoleLayout";
import { DISPLAY_VERSION } from "../shared/appConfig";
import { useText } from "../shared/i18n";
import { useAuth } from "./auth";

export function AppShell() {
  const t = useText();
  const { identity, logout } = useAuth();
  const [appearanceOpen, setAppearanceOpen] = useState(false);

  return (
    <>
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
            <WorkbenchThemeToggle />
            <Tooltip title={t.app.appearance}>
              <Button
                aria-label={t.app.appearance}
                className="appearance-drawer-trigger"
                icon={<BgColorsOutlined />}
                type="text"
                onClick={() => setAppearanceOpen(true)}
              />
            </Tooltip>
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
      <Drawer
        open={appearanceOpen}
        rootClassName="appearance-drawer"
        size={760}
        title={<Flex align="center" gap="small"><BgColorsOutlined />{t.app.appearance}</Flex>}
        onClose={() => setAppearanceOpen(false)}
      >
        <WorkbenchAppearanceSettings />
      </Drawer>
    </>
  );
}
