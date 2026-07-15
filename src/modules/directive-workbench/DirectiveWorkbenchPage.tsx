import { CopyOutlined, ImportOutlined } from "@ant-design/icons";
import { WorkbenchPage, WorkbenchPanel } from "@lwmacct/260627-antd-workbench";
import { Alert, App as AntdApp, Button, Flex, Space, Tabs, Tag } from "antd";
import { useMemo } from "react";
import { useText } from "../../shared/i18n";
import { StructuredEditorPanel } from "./components/StructuredEditorPanel";
import { RequestDebuggerPanel } from "./components/RequestDebuggerPanel";
import { SourceEditor } from "./components/SourceEditor";
import { useDirectiveEditor } from "./hooks/useDirectiveEditor";
import { copyText, formatPayload } from "./utils";

export function DirectiveWorkbenchPage() {
  const t = useText();
  const { message } = AntdApp.useApp();
  const state = useDirectiveEditor(t.authConsole);
  const sourceDirty = state.activeSource === "payload"
    ? state.payloadInput !== formatPayload(state.payload)
    : state.tokenInput !== state.directiveToken;
  const items = useMemo(() => state.editor.source === "inline" ? [
    { key: "payload", label: "Payload JSON", children: <SourceEditor placeholder='{ "target": { "url": "https://api.example.com" } }' value={state.payloadInput} onChange={state.setPayloadInput} /> },
    { key: "token", label: "Token", children: <SourceEditor placeholder="dproxy.<version>.i..." value={state.tokenInput} onChange={state.setTokenInput} /> },
  ] : [{ key: "token", label: "Token", children: <SourceEditor placeholder="dproxy.<version>.r..." value={state.tokenInput} onChange={state.setTokenInput} /> }], [state.editor.source, state.payloadInput, state.tokenInput]);
  return <WorkbenchPage description={t.authConsole.description} title={t.app.authConsole}>
    {state.error ? <Alert closable showIcon style={{ marginBottom: 16 }} title={state.error} type="error" onClose={() => state.setError(null)} /> : null}
    <Flex vertical gap={16}>
      <WorkbenchPanel title={t.authConsole.structured}><StructuredEditorPanel editor={state.editor} text={t.authConsole} onUpdate={state.updateEditor} /></WorkbenchPanel>
      <WorkbenchPanel title={t.authConsole.editableSources}>
        <Tabs activeKey={state.activeSource} items={items} onChange={(key: string) => state.setActiveSource(key as "payload" | "token")} />
        <Flex align="center" gap="small" justify="space-between" wrap>
          <Tag color={sourceDirty ? "orange" : "green"}>{sourceDirty ? t.authConsole.dirty : t.authConsole.synced}</Tag>
          <Space wrap>
            <Button icon={<CopyOutlined />} onClick={() => void copyText(state.activeSource === "payload" ? state.payloadInput : state.tokenInput).then((ok) => void (ok ? message.success(t.authConsole.copied) : message.error(t.authConsole.copyFailed)))}>{state.activeSource === "payload" ? t.authConsole.copyPayload : t.authConsole.copyToken}</Button>
            <Button icon={<ImportOutlined />} onClick={state.activeSource === "payload" ? state.applyPayloadInput : state.applyTokenInput} type="primary">{state.activeSource === "payload" ? t.authConsole.applyPayload : t.authConsole.parseToken}</Button>
          </Space>
        </Flex>
      </WorkbenchPanel>
    </Flex>
    <RequestDebuggerPanel text={t.authConsole} directiveToken={state.directiveToken} />
  </WorkbenchPage>;
}
