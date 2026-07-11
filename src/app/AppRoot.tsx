import { WorkbenchProvider } from "@lwmacct/260627-antd-workbench";
import { HashRouter } from "react-router-dom";
import { AppShell } from "./AppShell";
import { AuthBoundary } from "./auth";

export function AppRoot() {
  return (
    <WorkbenchProvider
      appearance={{ storageKey: "dproxy-theme" }}
      defaultLocale="zh-CN"
      localeStorageKey="dproxy-locale"
      withAntdApp
    >
      <AuthBoundary>
        <HashRouter>
          <AppShell />
        </HashRouter>
      </AuthBoundary>
    </WorkbenchProvider>
  );
}
