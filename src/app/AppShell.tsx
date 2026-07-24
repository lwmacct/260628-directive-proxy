import { GithubOutlined } from "@ant-design/icons";
import {
  WorkbenchAppearanceButton,
  WorkbenchLanguageToggle,
  WorkbenchShell,
} from "@lwmacct/260627-antd-workbench";
import { Button, Tooltip } from "antd";
import { Navigate, Route, Routes } from "react-router-dom";
import { ConsoleLayout } from "../modules/console/ConsoleLayout";
import { DISPLAY_VERSION } from "../shared/appConfig";
import { useText } from "../shared/i18n";

const SOURCE_URL = "https://github.com/lwmacct/260628-directive-proxy";

export function AppShell() {
  const text = useText();
  return (
    <WorkbenchShell
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
          <Tooltip title={text.app.sourceCode}>
            <Button
              aria-label={text.app.sourceCode}
              className="wb-header-action"
              href={SOURCE_URL}
              icon={<GithubOutlined />}
              rel="noopener noreferrer"
              target="_blank"
            />
          </Tooltip>
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
