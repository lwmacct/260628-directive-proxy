import { WorkbenchProvider } from "@lwmacct/260627-antd-workbench";
import zhCN from "antd/es/locale/zh_CN";
import { HashRouter } from "react-router-dom";
import { AppShell } from "./AppShell";

export function AppRoot() {
  return (
    <WorkbenchProvider
      appearance={{ storageKey: "dproxy-theme" }}
      locale={{
        defaultValue: "zh-CN",
        documentLang: "zh-CN",
        options: [
          {
            antdLocale: zhCN,
            documentLang: "zh-CN",
            label: "简体中文",
            shortLabel: "中",
            value: "zh-CN",
          },
        ],
        storageKey: "dproxy-locale",
      }}
      withAntdApp
    >
      <HashRouter>
        <AppShell />
      </HashRouter>
    </WorkbenchProvider>
  );
}
