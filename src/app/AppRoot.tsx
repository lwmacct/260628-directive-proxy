import { WorkbenchProvider } from "@lwmacct/260627-antd-workbench";
import { HashRouter } from "react-router-dom";
import { AppShell } from "./AppShell";
import { AuthBoundary } from "./auth";
import type { SessionState } from "./session";

export function AppRoot({ initialSession }: { initialSession: SessionState }) {
  return (
    <WorkbenchProvider
      appearance={{ storageKey: "dp-theme" }}
      defaultLocale="zh-CN"
      localeStorageKey="dp-locale"
      withAntdApp
    >
      <AuthBoundary initialSession={initialSession}>
        <HashRouter>
          <AppShell />
        </HashRouter>
      </AuthBoundary>
    </WorkbenchProvider>
  );
}
