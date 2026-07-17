import { ApiOutlined, CodeOutlined, DatabaseOutlined, FileTextOutlined, SwapOutlined } from "@ant-design/icons";
import { WorkbenchSectionLayout } from "@lwmacct/260627-antd-workbench";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { DirectiveBuilderPage } from "../directive-workbench/DirectiveBuilderPage";
import { PayloadCodecPage } from "../directive-workbench/PayloadCodecPage";
import type { DirectiveSource } from "../directive-workbench/types";
import { useText } from "../../shared/i18n";

type ConsoleSectionKey = DirectiveSource | "payload-codec";

const sectionKeys = new Set<ConsoleSectionKey>(["inline", "http", "redis", "file", "payload-codec"]);

function activeSection(pathname: string): ConsoleSectionKey {
  const key = pathname.split("/")[2];
  if (sectionKeys.has(key as ConsoleSectionKey)) {
    return key as ConsoleSectionKey;
  }
  return "inline";
}

export function ConsoleLayout() {
  const t = useText();
  const location = useLocation();
  const navigate = useNavigate();
  const inlineItems = [
    { key: "inline", label: t.authConsole.inlineSource, icon: <CodeOutlined /> },
    { key: "payload-codec", label: t.authConsole.payloadCodec, icon: <SwapOutlined /> },
  ] as const;
  const remoteItems = [
    { key: "http", label: t.authConsole.httpSource, icon: <ApiOutlined /> },
    { key: "redis", label: t.authConsole.redisSource, icon: <DatabaseOutlined /> },
    { key: "file", label: t.authConsole.fileSource, icon: <FileTextOutlined /> },
  ] as const;

  return (
    <WorkbenchSectionLayout
      selectedKey={activeSection(location.pathname)}
      nav={[
        {
          type: "group",
          key: "payload-tools",
          label: t.authConsole.payloadGroup,
          children: [...inlineItems],
        },
        {
          type: "group",
          key: "remote-tools",
          label: t.authConsole.remoteGroup,
          children: [...remoteItems],
        },
      ]}
      onSelect={(key) => navigate(`/console/${key}`)}
    >
      <Routes>
        <Route element={<Navigate replace to="inline" />} index />
        <Route element={<DirectiveBuilderPage source="inline" />} path="inline" />
        <Route element={<DirectiveBuilderPage source="http" />} path="http" />
        <Route element={<DirectiveBuilderPage source="redis" />} path="redis" />
        <Route element={<DirectiveBuilderPage source="file" />} path="file" />
        <Route element={<PayloadCodecPage />} path="payload-codec" />
        <Route element={<Navigate replace to="inline" />} path="*" />
      </Routes>
    </WorkbenchSectionLayout>
  );
}
