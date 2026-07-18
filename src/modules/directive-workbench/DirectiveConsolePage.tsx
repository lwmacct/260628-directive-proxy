import { ApiOutlined, CodeOutlined, CopyOutlined, DatabaseOutlined, FileTextOutlined, KeyOutlined, SendOutlined, ThunderboltOutlined } from "@ant-design/icons";
import { WorkbenchPage, WorkbenchPanel } from "@lwmacct/260627-antd-workbench";
import { Alert, App as AntdApp, Button, Drawer, Flex, Input, Segmented, Space, Tag, Typography } from "antd";
import { useState, type ChangeEvent } from "react";
import type { Text } from "../../shared/i18n";
import { useText } from "../../shared/i18n";
import { TOKEN_FAMILY, TOKEN_VERSION } from "./codec";
import { RequestDebugger } from "./components/RequestDebugger";
import { StructuredEditorPanel } from "./components/StructuredEditorPanel";
import { useDirectiveSession } from "./hooks/useDirectiveSession";
import type { DirectiveSource } from "./types";
import { copyText } from "./utils";

type SourceContent = {
  builderTitle: string;
  documentTitle: string;
  jsonPlaceholder: string;
};

function sourceContent(text: Text["authConsole"], source: DirectiveSource): SourceContent {
  switch (source) {
    case "inline":
      return {
        builderTitle: text.payloadBuilder,
        documentTitle: text.payloadJSON,
        jsonPlaceholder: '{ "target": { "base_url": "https://api.example.com" } }',
      };
    case "http":
      return {
        builderTitle: text.httpSpecBuilder,
        documentTitle: text.remoteSpecJSON,
        jsonPlaceholder: '{ "http": { "url": "https://resolver.example.com" } }',
      };
    case "redis":
      return {
        builderTitle: text.redisSpecBuilder,
        documentTitle: text.remoteSpecJSON,
        jsonPlaceholder: '{ "redis": { "url": "redis://localhost:6379/0", "key": "service/primary" } }',
      };
    case "file":
      return {
        builderTitle: text.fileSpecBuilder,
        documentTitle: text.remoteSpecJSON,
        jsonPlaceholder: '{ "file": { "path": "services/primary.json" } }',
      };
  }
}

function StatusTag(props: { error: string | null; pending?: boolean; text: Text["authConsole"] }) {
  if (props.error) return <Tag color="red">{props.text.invalid}</Tag>;
  if (props.pending) return <Tag>{props.text.pending}</Tag>;
  return <Tag color="green">{props.text.valid}</Tag>;
}

export function DirectiveConsolePage() {
  const t = useText();
  const { message } = AntdApp.useApp();
  const session = useDirectiveSession(t.authConsole);
  const [debuggerOpen, setDebuggerOpen] = useState(false);
  const content = sourceContent(t.authConsole, session.source);
  const documentError = session.jsonError ?? session.formError;
  const tokenPrefix = `${TOKEN_FAMILY}.${TOKEN_VERSION}.${session.envelope.kind}`;

  function copy(value: string) {
    void copyText(value).then((ok) => void (ok ? message.success(t.authConsole.copied) : message.error(t.authConsole.copyFailed)));
  }

  return <WorkbenchPage
    className="directive-console-page"
    description={<span className="directive-console-header__description">{t.authConsole.directiveConsoleDescription}</span>}
    extra={<Space className="directive-console-header__actions" wrap>
      <div className="directive-console-header__protocol">
        <span>{t.authConsole.currentDirective}</span>
        <Tag variant="filled">{tokenPrefix}</Tag>
      </div>
      <Button icon={<SendOutlined />} onClick={() => setDebuggerOpen(true)} type="primary">{t.authConsole.requestDebug}</Button>
    </Space>}
    title={<span className="directive-console-header__title"><ThunderboltOutlined />{t.authConsole.directiveConsole}</span>}
  >
    <div className="directive-source-control">
      <Segmented
        block
        options={[
          { icon: <CodeOutlined />, label: "Inline", value: "inline" },
          { icon: <ApiOutlined />, label: "HTTP", value: "http" },
          { icon: <DatabaseOutlined />, label: "Redis", value: "redis" },
          { icon: <FileTextOutlined />, label: "File", value: "file" },
        ]}
        value={session.source}
        onChange={(value: string | number) => session.setSource(value as DirectiveSource)}
      />
    </div>
    <WorkbenchPanel extra={<Tag color="cyan">{t.authConsole.localOnly}</Tag>} title={content.builderTitle}>
      {session.formError ? <Alert showIcon style={{ marginBottom: 16 }} title={t.authConsole.invalidFormDetail(session.formError)} type="warning" /> : null}
      <StructuredEditorPanel editor={session.editor} source={session.source} text={t.authConsole} onUpdate={session.updateEditor} />
    </WorkbenchPanel>
    <div className="directive-console-secret">
      <div className="directive-console-secret__identity">
        <span className="directive-console-secret__icon"><KeyOutlined /></span>
        <div className="directive-console-secret__copy">
          <Flex align="center" gap="small" wrap>
            <Typography.Text strong>{t.authConsole.tokenSecret}</Typography.Text>
            <Tag variant="filled">HMAC-SHA256</Tag>
          </Flex>
          <Typography.Text type="secondary">{t.authConsole.tokenSecretDescription}</Typography.Text>
        </div>
      </div>
      <Input.Password
        aria-label={t.authConsole.tokenSecret}
        className="directive-console-secret__input"
        placeholder={t.authConsole.tokenSecretPlaceholder}
        size="large"
        value={session.tokenSecret}
        onChange={(event: ChangeEvent<HTMLInputElement>) => session.updateTokenSecret(event.target.value)}
      />
    </div>
    <div className="directive-codec-grid">
      <WorkbenchPanel
        className="directive-codec-panel"
        extra={<Flex align="center" gap="small">
          <StatusTag error={documentError} text={t.authConsole} />
          <Button icon={<CopyOutlined />} onClick={() => copy(session.jsonInput)}>{t.authConsole.copyJSON}</Button>
        </Flex>}
        title={content.documentTitle}
      >
        <Input.TextArea
          aria-label={content.documentTitle}
          autoSize={{ minRows: 16 }}
          className="directive-codec-input source-input"
          placeholder={content.jsonPlaceholder}
          spellCheck={false}
          value={session.jsonInput}
          onChange={(event: ChangeEvent<HTMLTextAreaElement>) => session.updateJSON(event.target.value)}
        />
        {documentError ? <Alert showIcon title={documentError} type="error" /> : null}
      </WorkbenchPanel>
      <WorkbenchPanel
        className="directive-codec-panel"
        extra={<Flex align="center" gap="small">
          <StatusTag error={session.tokenError} pending={!session.directiveToken} text={t.authConsole} />
          <Button disabled={!session.tokenInput} icon={<CopyOutlined />} onClick={() => copy(session.tokenInput)}>{t.authConsole.copyToken}</Button>
        </Flex>}
        title="Token"
      >
        <Input.TextArea
          aria-label="Token"
          autoSize={{ minRows: 16 }}
          className="directive-codec-input source-input"
          placeholder={`Bearer ${tokenPrefix}.<base64url-json>.<hmac>`}
          spellCheck={false}
          value={session.tokenInput}
          onChange={(event: ChangeEvent<HTMLTextAreaElement>) => void session.updateToken(event.target.value)}
        />
        {session.tokenError ? <Alert showIcon title={session.tokenError} type="error" /> : null}
      </WorkbenchPanel>
    </div>
    <Drawer
      open={debuggerOpen}
      rootClassName="request-debugger-drawer"
      size={760}
      title={t.authConsole.requestDebug}
      onClose={() => setDebuggerOpen(false)}
    >
      <RequestDebugger directiveToken={session.directiveToken} text={t.authConsole} />
    </Drawer>
  </WorkbenchPage>;
}
