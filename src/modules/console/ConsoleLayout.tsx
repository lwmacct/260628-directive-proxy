import { ApiOutlined } from "@ant-design/icons";
import { WorkbenchSectionLayout } from "@lwmacct/260627-antd-workbench";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { DirectiveWorkbenchPage } from "../directive-workbench/DirectiveWorkbenchPage";
import { useText } from "../../shared/i18n";

type ConsoleSectionKey = "auth-console";

const sectionKeys = new Set<ConsoleSectionKey>(["auth-console"]);

function activeSection(pathname: string): ConsoleSectionKey {
  const key = pathname.split("/")[2];
  if (sectionKeys.has(key as ConsoleSectionKey)) {
    return key as ConsoleSectionKey;
  }
  return "auth-console";
}

export function ConsoleLayout() {
  const t = useText();
  const location = useLocation();
  const navigate = useNavigate();
  const sectionItems = [
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
        <Route element={<Navigate replace to="auth-console" />} index />
        <Route element={<DirectiveWorkbenchPage />} path="auth-console" />
        <Route element={<Navigate replace to="auth-console" />} path="*" />
      </Routes>
    </WorkbenchSectionLayout>
  );
}
