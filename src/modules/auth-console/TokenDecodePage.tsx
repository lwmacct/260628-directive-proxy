import { CopyOutlined } from "@ant-design/icons";
import { WorkbenchPage } from "@lwmacct/260627-antd-workbench";
import { Alert, Button, Card, Col, Form, Input, Row, Typography, message } from "antd";
import { useMemo, useState } from "react";

const { Paragraph, Text } = Typography;
const tokenPrefix = "dpx1.";

export function TokenDecodePage() {
  const [token, setToken] = useState("");

  const result = useMemo(() => decodeToken(token), [token]);
  const output = result.ok ? JSON.stringify(result.payload, null, 2) : "";

  return (
    <WorkbenchPage
      description="输入不含 Authorization: Bearer 前缀的 dpx1 token，解析 directive payload。"
      extra={
        <Button
          disabled={!output}
          icon={<CopyOutlined />}
          onClick={() => void copyText(output).then(reportCopyResult)}
          type="primary"
        >
          Copy Payload
        </Button>
      }
      title="Token 反解析"
    >
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card size="small" title="Token">
            <Form layout="vertical">
              <Form.Item label="dpx1 token">
                <Input.TextArea
                  autoSize={{ minRows: 8, maxRows: 16 }}
                  onChange={(event) => setToken(event.target.value)}
                  placeholder="dpx1.<base64url-json>"
                  value={token}
                />
              </Form.Item>
            </Form>
            <Text type="secondary">不要包含 Authorization: Bearer。</Text>
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          {result.ok ? (
            <Card size="small" title="Payload JSON">
              <Paragraph className="code-output">{output}</Paragraph>
            </Card>
          ) : (
            <Alert
              message={result.error || "等待输入 token"}
              showIcon
              type={token.trim() ? "error" : "info"}
            />
          )}
        </Col>
      </Row>
    </WorkbenchPage>
  );
}

type DecodeResult =
  | { ok: true; payload: unknown }
  | { ok: false; error: string };

function decodeToken(value: string): DecodeResult {
  const token = value.trim();
  if (!token) {
    return { ok: false, error: "" };
  }
  if (!token.startsWith(tokenPrefix)) {
    return { ok: false, error: "token 必须以 dpx1. 开头" };
  }
  try {
    const raw = token.slice(tokenPrefix.length);
    const json = new TextDecoder().decode(base64URLDecode(raw));
    return { ok: true, payload: JSON.parse(json) };
  } catch (err) {
    return {
      ok: false,
      error: err instanceof Error ? err.message : "token 解析失败",
    };
  }
}

function base64URLDecode(value: string) {
  const normalized = value.replaceAll("-", "+").replaceAll("_", "/");
  const padded = normalized.padEnd(normalized.length + ((4 - normalized.length % 4) % 4), "=");
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

function reportCopyResult(ok: boolean) {
  if (ok) {
    void message.success("已复制");
    return;
  }
  void message.error("复制失败");
}

async function copyText(value: string) {
  if (navigator.clipboard?.writeText && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(value);
      return true;
    } catch {
      // Fall through to the legacy path below.
    }
  }

  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  textarea.style.top = "0";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  try {
    return document.execCommand("copy");
  } finally {
    document.body.removeChild(textarea);
  }
}
