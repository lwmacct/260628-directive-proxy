import { WorkbenchProvider } from "@lwmacct/260627-antd-workbench";
import { HashRouter } from "react-router-dom";
import { AppShell } from "./AppShell";

export function AppRoot() {
  return (
    <WorkbenchProvider
      appearance={{ storageKey: "dp-theme" }}
      defaultLocale="zh-CN"
      localeStorageKey="dp-locale"
      withAntdApp
    >
      <HashRouter>
        <AppShell />
      </HashRouter>
    </WorkbenchProvider>
  );
}
