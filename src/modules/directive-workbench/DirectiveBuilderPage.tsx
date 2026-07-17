import { CopyOutlined, ImportOutlined } from "@ant-design/icons";
import { WorkbenchPage, WorkbenchPanel } from "@lwmacct/260627-antd-workbench";
import { Alert, App as AntdApp, Button, Flex, Input, Space, Tabs, Tag } from "antd";
import { useMemo } from "react";
import type { Text } from "../../shared/i18n";
import { useText } from "../../shared/i18n";
import { formatDirectiveJSON } from "./codec";
import { RequestDebuggerPanel } from "./components/RequestDebuggerPanel";
import { SourceEditor } from "./components/SourceEditor";
import { StructuredEditorPanel } from "./components/StructuredEditorPanel";
import { useDirectiveEditor } from "./hooks/useDirectiveEditor";
import type { DirectiveSource } from "./types";
import { copyText } from "./utils";

type PageContent = {
  builderTitle: string;
  description: string;
  jsonPlaceholder: string;
  title: string;
};

function pageContent(text: Text["authConsole"], source: DirectiveSource): PageContent {
  switch (source) {
    case "inline":
      return {
        builderTitle: text.payloadBuilder,
        description: text.inlineDescription,
        jsonPlaceholder: '{ "target": { "url": "https://api.example.com" } }',
        title: text.inlineSource,
      };
    case "http":
      return {
        builderTitle: text.httpSpecBuilder,
        description: text.httpDescription,
        jsonPlaceholder: '{ "http": { "url": "https://resolver.example.com" } }',
        title: text.httpSource,
      };
    case "redis":
      return {
        builderTitle: text.redisSpecBuilder,
        description: text.redisDescription,
        jsonPlaceholder: '{ "redis": { "url": "redis://localhost:6379/0", "key": "service/primary" } }',
        title: text.redisSource,
      };
    case "file":
      return {
        builderTitle: text.fileSpecBuilder,
        description: text.fileDescription,
        jsonPlaceholder: '{ "file": { "path": "services/primary.json" } }',
        title: text.fileSource,
      };
  }
}

function DirectiveBuilderSession({ source }: { source: DirectiveSource }) {
  const t = useText();
  const content = pageContent(t.authConsole, source);
  const { message } = AntdApp.useApp();
  const state = useDirectiveEditor(t.authConsole, source);
  const sourceDirty = state.activeSource === "json"
    ? state.jsonInput !== formatDirectiveJSON(state.envelope)
    : state.tokenInput !== state.directiveToken;
  const tokenPrefix = `dp.19.${state.envelope.kind}`;
  const items = useMemo(() => [
    {
      key: "json",
      label: t.authConsole.tokenJSON,
      children: <SourceEditor placeholder={content.jsonPlaceholder} value={state.jsonInput} onChange={state.setJSONInput} />,
    },
    {
      key: "token",
      label: "Token",
      children: <SourceEditor placeholder={`${tokenPrefix}.<base64url-json>.<hmac>`} value={state.tokenInput} onChange={state.setTokenInput} />,
    },
  ], [content.jsonPlaceholder, state.jsonInput, state.tokenInput, t.authConsole.tokenJSON, tokenPrefix]);
  const activeValue = state.activeSource === "json" ? state.jsonInput : state.tokenInput;

  return <WorkbenchPage description={content.description} title={content.title}>
    {state.error ? <Alert closable showIcon style={{ marginBottom: 16 }} title={state.error} type="error" onClose={() => state.setError(null)} /> : null}
    {state.formError ? <Alert showIcon style={{ marginBottom: 16 }} title={t.authConsole.invalidFormDetail(state.formError)} type="warning" /> : null}
    <Flex vertical gap={16}>
      <WorkbenchPanel extra={<Tag color="cyan">{t.authConsole.localOnly}</Tag>} title={content.builderTitle}>
        <StructuredEditorPanel editor={state.editor} source={source} text={t.authConsole} onUpdate={state.updateEditor} />
      </WorkbenchPanel>
      <WorkbenchPanel extra={<Tag>{tokenPrefix}</Tag>} title={t.authConsole.directiveEncoding}>
        <Flex align="center" gap="small" style={{ marginBottom: 12 }}>
          <span>{t.authConsole.tokenSecret}</span>
          <Input.Password aria-label={t.authConsole.tokenSecret} placeholder={t.authConsole.tokenSecretPlaceholder} value={state.tokenSecret} onChange={(event) => state.setTokenSecret(event.target.value)} />
        </Flex>
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

export function DirectiveBuilderPage({ source }: { source: DirectiveSource }) {
  return <DirectiveBuilderSession key={source} source={source} />;
}
