import { CopyOutlined, SwapOutlined } from "@ant-design/icons";
import { WorkbenchPage, WorkbenchPanel } from "@lwmacct/260627-antd-workbench";
import { Alert, App as AntdApp, Button, Flex, Form, Input, Tag } from "antd";
import { useRef, useState, type ChangeEvent } from "react";
import { useText } from "../../shared/i18n";
import { decodeDirective, encodeDirective, formatDirectiveJSON, parseDirectiveJSON } from "./codec";
import type { DirectiveEnvelope } from "./types";
import { copyText, errorMessage } from "./utils";

const INITIAL_PAYLOAD = `{
  "target": {
    "url": "https://api.example.com"
  }
}`;

type Direction = "payload" | "token";

function StatusTag({ error, pending, text }: { error: string | null; pending?: boolean; text: ReturnType<typeof useText>["authConsole"] }) {
  if (error) return <Tag color="red">{text.invalid}</Tag>;
  if (pending) return <Tag>{text.pending}</Tag>;
  return <Tag color="green">{text.valid}</Tag>;
}

export function PayloadCodecPage() {
  const t = useText();
  const { message } = AntdApp.useApp();
  const [secret, setSecret] = useState("");
  const [payloadInput, setPayloadInput] = useState(INITIAL_PAYLOAD);
  const [tokenInput, setTokenInput] = useState("");
  const [payloadError, setPayloadError] = useState<string | null>(null);
  const [tokenError, setTokenError] = useState<string | null>(null);
  const direction = useRef<Direction>("payload");
  const generation = useRef(0);

  async function updateFromPayload(value: string, key: string) {
    const currentGeneration = ++generation.current;
    try {
      const envelope = parseDirectiveJSON("inline", value, t.authConsole);
      setPayloadError(null);
      if (!key.trim()) {
        setTokenInput("");
        setTokenError(null);
        return;
      }
      const token = await encodeDirective(envelope, key, t.authConsole);
      if (currentGeneration !== generation.current) return;
      setTokenInput(token);
      setTokenError(null);
    } catch (error) {
      if (currentGeneration !== generation.current) return;
      setPayloadError(errorMessage(error, t.authConsole.jsonParseFailed));
    }
  }

  async function updateFromToken(value: string, key: string) {
    const currentGeneration = ++generation.current;
    if (!value.trim()) {
      setTokenError(null);
      return;
    }
    if (!key.trim()) {
      setTokenError(t.authConsole.tokenSecretRequired);
      return;
    }
    try {
      const envelope = await decodeDirective(value, key, t.authConsole);
      if (currentGeneration !== generation.current) return;
      if (envelope.kind !== "inline") throw new Error(t.authConsole.payloadCodecInlineOnly);
      setPayloadInput(formatDirectiveJSON(envelope as Extract<DirectiveEnvelope, { kind: "inline" }>));
      setPayloadError(null);
      setTokenError(null);
    } catch (error) {
      if (currentGeneration !== generation.current) return;
      setTokenError(errorMessage(error, t.authConsole.tokenParseFailed));
    }
  }

  function changePayload(event: ChangeEvent<HTMLTextAreaElement>) {
    const value = event.target.value;
    direction.current = "payload";
    setPayloadInput(value);
    void updateFromPayload(value, secret);
  }

  function changeToken(event: ChangeEvent<HTMLTextAreaElement>) {
    const value = event.target.value;
    direction.current = "token";
    setTokenInput(value);
    void updateFromToken(value, secret);
  }

  function changeSecret(event: ChangeEvent<HTMLInputElement>) {
    const value = event.target.value;
    setSecret(value);
    if (direction.current === "payload") void updateFromPayload(payloadInput, value);
    else void updateFromToken(tokenInput, value);
  }

  function copy(value: string) {
    void copyText(value).then((ok) => void (ok ? message.success(t.authConsole.copied) : message.error(t.authConsole.copyFailed)));
  }

  return <WorkbenchPage
    description={t.authConsole.payloadCodecDescription}
    extra={<Tag icon={<SwapOutlined />}>dp.19.inline</Tag>}
    title={t.authConsole.payloadCodec}
  >
    <Form className="payload-codec-secret" layout="vertical">
      <Form.Item label={t.authConsole.tokenSecret}>
        <Input.Password aria-label={t.authConsole.tokenSecret} placeholder={t.authConsole.tokenSecretPlaceholder} value={secret} onChange={changeSecret} />
      </Form.Item>
    </Form>
    <div className="payload-codec-grid">
      <WorkbenchPanel
        className="payload-codec-panel"
        extra={<StatusTag error={payloadError} text={t.authConsole} />}
        title={t.authConsole.payloadJSON}
      >
        <Input.TextArea aria-label={t.authConsole.payloadJSON} className="payload-codec-input source-input" spellCheck={false} value={payloadInput} onChange={changePayload} />
        {payloadError ? <Alert showIcon title={payloadError} type="error" /> : null}
        <Flex justify="end"><Button icon={<CopyOutlined />} onClick={() => copy(payloadInput)}>{t.authConsole.copyJSON}</Button></Flex>
      </WorkbenchPanel>
      <WorkbenchPanel
        className="payload-codec-panel"
        extra={<StatusTag error={tokenError} pending={!tokenInput} text={t.authConsole} />}
        title="Token"
      >
        <Input.TextArea aria-label="Token" className="payload-codec-input source-input" placeholder="Bearer dp.19.inline.<base64url-json>.<hmac>" spellCheck={false} value={tokenInput} onChange={changeToken} />
        {tokenError ? <Alert showIcon title={tokenError} type="error" /> : null}
        <Flex justify="end"><Button icon={<CopyOutlined />} onClick={() => copy(tokenInput)}>{t.authConsole.copyToken}</Button></Flex>
      </WorkbenchPanel>
    </div>
  </WorkbenchPage>;
}
