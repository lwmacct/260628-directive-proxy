import { ApiOutlined, HistoryOutlined } from "@ant-design/icons";
import { WorkbenchSectionLayout } from "@lwmacct/260627-antd-workbench";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { AuthConsolePage } from "../auth-console/AuthConsolePage";
import { ExchangesPage } from "../exchanges/ExchangesPage";
import { useText } from "../../shared/i18n";

type ConsoleSectionKey = "auth-console" | "exchanges";

const sectionKeys = new Set<ConsoleSectionKey>(["exchanges", "auth-console"]);

function activeSection(pathname: string): ConsoleSectionKey {
  const key = pathname.split("/")[2];
  if (sectionKeys.has(key as ConsoleSectionKey)) {
    return key as ConsoleSectionKey;
  }
  return "exchanges";
}

export function ConsoleLayout() {
  const t = useText();
  const location = useLocation();
  const navigate = useNavigate();
  const sectionItems = [
    { key: "exchanges", label: t.app.exchanges, icon: <HistoryOutlined /> },
    { key: "auth-console", label: t.app.authConsole, icon: <ApiOutlined /> },
  ] as const;

  return (
    <WorkbenchSectionLayout
      selectedKey={activeSection(location.pathname)}
      nav={[
        {
          type: "group",
          key: "debug-tools",
          label: t.app.debugTools,
          children: [...sectionItems],
        },
      ]}
      onSelect={(key) => navigate(`/console/${key}`)}
    >
      <Routes>
        <Route element={<Navigate replace to="exchanges" />} index />
        <Route element={<ExchangesPage />} path="exchanges" />
        <Route element={<AuthConsolePage />} path="auth-console" />
        <Route element={<Navigate replace to="exchanges" />} path="*" />
      </Routes>
    </WorkbenchSectionLayout>
  );
}
