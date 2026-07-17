import { CopyOutlined, ImportOutlined } from "@ant-design/icons";
import { WorkbenchPage, WorkbenchPanel } from "@lwmacct/260627-antd-workbench";
import { Alert, App as AntdApp, Button, Flex, Space, Tabs, Tag } from "antd";
import { useMemo } from "react";
import { useText } from "../../shared/i18n";
import { formatDirectiveJSON } from "./codec";
import { RequestDebuggerPanel } from "./components/RequestDebuggerPanel";
import { SourceEditor } from "./components/SourceEditor";
import { StructuredEditorPanel } from "./components/StructuredEditorPanel";
import { useDirectiveEditor } from "./hooks/useDirectiveEditor";
import { copyText } from "./utils";

export function DirectiveWorkbenchPage() {
  const t = useText();
  const { message } = AntdApp.useApp();
  const state = useDirectiveEditor(t.authConsole);
  const sourceDirty = state.activeSource === "json"
    ? state.jsonInput !== formatDirectiveJSON(state.envelope)
    : state.tokenInput !== state.directiveToken;
  const tokenPrefix = `dp.18.${state.envelope.kind}`;
  const items = useMemo(() => [
    {
      key: "json",
      label: t.authConsole.tokenJSON,
      children: <SourceEditor
        placeholder={state.envelope.kind === "inline" ? '{ "target": { "url": "https://api.example.com" } }' : '{ "http": { "url": "https://resolver.example.com" } }'}
        value={state.jsonInput}
        onChange={state.setJSONInput}
      />,
    },
    {
      key: "token",
      label: "Token",
      children: <SourceEditor placeholder={`${tokenPrefix}.<base64url-json>`} value={state.tokenInput} onChange={state.setTokenInput} />,
    },
  ], [state.envelope.kind, state.jsonInput, state.tokenInput, t.authConsole.tokenJSON, tokenPrefix]);
  const activeValue = state.activeSource === "json" ? state.jsonInput : state.tokenInput;
  return <WorkbenchPage description={t.authConsole.description} title={t.app.authConsole}>
    {state.error ? <Alert closable showIcon style={{ marginBottom: 16 }} title={state.error} type="error" onClose={() => state.setError(null)} /> : null}
    {state.formError ? <Alert showIcon style={{ marginBottom: 16 }} title={t.authConsole.invalidFormDetail(state.formError)} type="warning" /> : null}
    <Flex vertical gap={16}>
      <WorkbenchPanel extra={<Tag color="cyan">{t.authConsole.localOnly}</Tag>} title={t.authConsole.structured}>
        <StructuredEditorPanel editor={state.editor} text={t.authConsole} onUpdate={state.updateEditor} />
      </WorkbenchPanel>
      <WorkbenchPanel
        extra={<Tag>{tokenPrefix}</Tag>}
        title={t.authConsole.editableSources}
      >
        <Alert showIcon style={{ marginBottom: 12 }} title={t.authConsole.tokenJSONDescription(tokenPrefix)} type="info" />
        <Tabs activeKey={state.activeSource} items={items} onChange={(key: string) => state.setActiveSource(key as "json" | "token")} />
        <Flex align="center" gap="small" justify="space-between" wrap>
          <Tag color={sourceDirty ? "orange" : "green"}>{sourceDirty ? t.authConsole.dirty : t.authConsole.synced}</Tag>
          <Space wrap>
            <Button icon={<CopyOutlined />} onClick={() => void copyText(activeValue).then((ok) => void (ok ? message.success(t.authConsole.copied) : message.error(t.authConsole.copyFailed)))}>
              {state.activeSource === "json" ? t.authConsole.copyJSON : t.authConsole.copyToken}
            </Button>
            <Button icon={<ImportOutlined />} onClick={state.activeSource === "json" ? state.applyJSONInput : state.applyTokenInput} type="primary">
              {state.activeSource === "json" ? t.authConsole.applyJSON : t.authConsole.parseToken}
            </Button>
          </Space>
        </Flex>
      </WorkbenchPanel>
    </Flex>
    <RequestDebuggerPanel text={t.authConsole} directiveToken={state.directiveToken} />
  </WorkbenchPage>;
}
