import { ApiOutlined, HistoryOutlined, CodeOutlined } from "@ant-design/icons";
import { WorkbenchSectionLayout } from "@lwmacct/260627-antd-workbench";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { AuthConsolePage } from "../auth-console/AuthConsolePage";
import { TokenDecodePage } from "../auth-console/TokenDecodePage";
import { ExchangesPage } from "../exchanges/ExchangesPage";

type ConsoleSectionKey = "auth-console" | "exchanges" | "token-decode";

const sectionItems = [
  { key: "exchanges", label: "请求记录", icon: <HistoryOutlined /> },
  { key: "auth-console", label: "Authorization 生成器", icon: <ApiOutlined /> },
  { key: "token-decode", label: "Token 反解析", icon: <CodeOutlined /> },
] as const;

const sectionKeys = new Set<ConsoleSectionKey>(sectionItems.map((item) => item.key));

function activeSection(pathname: string): ConsoleSectionKey {
  const key = pathname.split("/")[2];
  if (sectionKeys.has(key as ConsoleSectionKey)) {
    return key as ConsoleSectionKey;
  }
  return "exchanges";
}

export function ConsoleLayout() {
  const location = useLocation();
  const navigate = useNavigate();

  return (
    <WorkbenchSectionLayout
      selectedKey={activeSection(location.pathname)}
      nav={[
        {
          type: "group",
          key: "debug-tools",
          label: "调试工具",
          children: [...sectionItems],
        },
      ]}
      onSelect={(key) => navigate(`/console/${key}`)}
    >
      <Routes>
        <Route element={<Navigate replace to="exchanges" />} index />
        <Route element={<ExchangesPage />} path="exchanges" />
        <Route element={<AuthConsolePage />} path="auth-console" />
        <Route element={<TokenDecodePage />} path="token-decode" />
        <Route element={<Navigate replace to="exchanges" />} path="*" />
      </Routes>
    </WorkbenchSectionLayout>
  );
}
