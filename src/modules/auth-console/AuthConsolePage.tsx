import {
  CopyOutlined,
  DeleteOutlined,
  ImportOutlined,
  PlusOutlined,
} from "@ant-design/icons";
import {
  WorkbenchPage,
  WorkbenchPanel,
} from "@lwmacct/260627-antd-workbench";
import {
  Alert,
  Button,
  Checkbox,
  Col,
  Flex,
  Form,
  Input,
  Row,
  Select,
  Space,
  Table,
  Tabs,
  Typography,
  message,
} from "antd";
import type { TableColumnsType } from "antd";
import type { CheckboxChangeEvent } from "antd/es/checkbox";
import { useMemo, useState, type ChangeEvent } from "react";

const { Text } = Typography;
const tokenPrefix = "dproxy.10.";

type HeaderOp = {
  key: string;
  op: "=" | "+" | "-";
  name: string;
  values: string;
};

type EditorState = {
  targetURL: string;
  joinPath: boolean;
  proxyURL: string;
  headerMode: "patch" | "replace";
  headerOps: HeaderOp[];
};

type DirectivePayload = {
  target: {
    url: string;
    join_path?: boolean;
  };
  proxy?: string;
  headers?: {
    mode?: "patch" | "replace";
    ops?: Array<{
      op: "=" | "+" | "-";
      name: string;
      values?: string[];
    }>;
  };
};

let headerOpID = 0;

const initialEditor: EditorState = {
  targetURL: "https://httpbin.org/anything",
  joinPath: true,
  proxyURL: "",
  headerMode: "patch",
  headerOps: [
    newHeaderOp("=", "Authorization", "Bearer upstream-token"),
    newHeaderOp("=", "X-Tenant", "tenant-a"),
  ],
};

export function AuthConsolePage() {
  const [editor, setEditor] = useState(initialEditor);
  const initialPayload = useMemo(() => buildPayload(initialEditor), []);
  const [payloadInput, setPayloadInput] = useState(() => formatPayload(initialPayload));
  const [tokenInput, setTokenInput] = useState(() => encodeToken(initialPayload));
  const [error, setError] = useState<string | null>(null);

  const payload = useMemo(() => buildPayload(editor), [editor]);
  const token = encodeToken(payload);

  function updateEditor(patch: Partial<EditorState>) {
    const next = { ...editor, ...patch };
    setEditor(next);
    syncInputs(buildPayload(next));
  }

  function syncInputs(nextPayload: DirectivePayload) {
    setPayloadInput(formatPayload(nextPayload));
    setTokenInput(encodeToken(nextPayload));
    setError(null);
  }

  function applyPayloadInput() {
    try {
      applyPayload(parsePayloadJSON(payloadInput));
      void message.success("Payload 已应用到表单和 Token");
    } catch (err) {
      setError(errorMessage(err, "Payload JSON 解析失败"));
    }
  }

  function applyTokenInput() {
    try {
      applyPayload(decodeToken(tokenInput));
      void message.success("Token 已解析并应用到表单和 Payload");
    } catch (err) {
      setError(errorMessage(err, "Token 解析失败"));
    }
  }

  function applyPayload(nextPayload: DirectivePayload) {
    setEditor(payloadToEditor(nextPayload));
    syncInputs(nextPayload);
  }

  function updateHeaderOp(key: string, patch: Partial<HeaderOp>) {
    updateEditor({
      headerOps: editor.headerOps.map((item) =>
        item.key === key ? { ...item, ...patch } : item,
      ),
    });
  }

  const columns: TableColumnsType<HeaderOp> = [
    {
      title: "Op",
      dataIndex: "op",
      width: 104,
      render: (_, record) => (
        <Select
          options={[
            { label: "Set", value: "=" },
            { label: "Add", value: "+" },
            { label: "Remove", value: "-" },
          ]}
          value={record.op}
          onChange={(op: HeaderOp["op"]) => updateHeaderOp(record.key, { op })}
        />
      ),
    },
    {
      title: "Name",
      dataIndex: "name",
      render: (_, record) => (
        <Input
          value={record.name}
          onChange={(event: ChangeEvent<HTMLInputElement>) =>
            updateHeaderOp(record.key, { name: event.target.value })
          }
        />
      ),
    },
    {
      title: "Values",
      dataIndex: "values",
      render: (_, record) => (
        <Input
          disabled={record.op === "-"}
          placeholder="comma separated"
          value={record.values}
          onChange={(event: ChangeEvent<HTMLInputElement>) =>
            updateHeaderOp(record.key, { values: event.target.value })
          }
        />
      ),
    },
    {
      title: "",
      key: "actions",
      width: 56,
      render: (_, record) => (
        <Button
          aria-label="Remove header op"
          icon={<DeleteOutlined />}
          onClick={() =>
            updateEditor({
              headerOps: editor.headerOps.filter((item) => item.key !== record.key),
            })
          }
          type="text"
        />
      ),
    },
  ];

  return (
    <WorkbenchPage
      description="从结构化表单、Payload JSON 或 Token 任一来源编辑 directive，并同步生成其他格式。"
      extra={
        <Button
          icon={<CopyOutlined />}
          onClick={() => void copyText(token).then(reportCopyResult)}
          type="primary"
        >
          复制 Token
        </Button>
      }
      title="Authorization 工作台"
    >
      {error ? (
        <Alert
          closable
          showIcon
          style={{ marginBottom: 16 }}
          title={error}
          type="error"
          onClose={() => setError(null)}
        />
      ) : null}

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={13}>
          <WorkbenchPanel title="结构化编辑">
            <Form layout="vertical">
              <Form.Item label="Target URL">
                <Input
                  value={editor.targetURL}
                  onChange={(event: ChangeEvent<HTMLInputElement>) =>
                    updateEditor({ targetURL: event.target.value })
                  }
                />
              </Form.Item>
              <Flex gap="small" wrap>
                <Form.Item label="Join Path">
                  <Checkbox
                    checked={editor.joinPath}
                    onChange={(event: CheckboxChangeEvent) =>
                      updateEditor({ joinPath: event.target.checked })
                    }
                  >
                    enabled
                  </Checkbox>
                </Form.Item>
                <Form.Item className="grow-field" label="Proxy">
                  <Input
                    allowClear
                    placeholder="socks5://user:pass@127.0.0.1:1080"
                    value={editor.proxyURL}
                    onChange={(event: ChangeEvent<HTMLInputElement>) =>
                      updateEditor({ proxyURL: event.target.value })
                    }
                  />
                </Form.Item>
                <Form.Item label="Header Mode">
                  <Select
                    options={[
                      { label: "Patch", value: "patch" },
                      { label: "Replace", value: "replace" },
                    ]}
                    value={editor.headerMode}
                    onChange={(headerMode: EditorState["headerMode"]) =>
                      updateEditor({ headerMode })
                    }
                  />
                </Form.Item>
              </Flex>
            </Form>

            <Flex align="center" justify="space-between" style={{ marginBottom: 12 }}>
              <Text strong>Header Ops</Text>
              <Button
                icon={<PlusOutlined />}
                onClick={() =>
                  updateEditor({ headerOps: [...editor.headerOps, newHeaderOp("=", "", "")] })
                }
              >
                Add
              </Button>
            </Flex>
            <Table<HeaderOp>
              columns={columns}
              dataSource={editor.headerOps}
              pagination={false}
              rowKey="key"
              scroll={{ x: 620 }}
              size="small"
            />
          </WorkbenchPanel>
        </Col>

        <Col xs={24} xl={11}>
          <Space orientation="vertical" size={14} style={{ width: "100%" }}>
            <WorkbenchPanel title="可编辑输入源">
              <Tabs
                items={[
                  {
                    key: "payload",
                    label: "Payload JSON",
                    children: (
                      <SourceEditor
                        actionLabel="应用 Payload"
                        placeholder='{ "target": { "url": "https://api.example.com" } }'
                        value={payloadInput}
                        onApply={applyPayloadInput}
                        onChange={setPayloadInput}
                      />
                    ),
                  },
                  {
                    key: "token",
                    label: "Token / Authorization",
                    children: (
                      <SourceEditor
                        actionLabel="解析 Token"
                        placeholder="dproxy.10...、Bearer dproxy.10... 或完整 Authorization header"
                        value={tokenInput}
                        onApply={applyTokenInput}
                        onChange={setTokenInput}
                      />
                    ),
                  },
                ]}
              />
            </WorkbenchPanel>
          </Space>
        </Col>
      </Row>
    </WorkbenchPage>
  );
}

function SourceEditor(props: {
  actionLabel: string;
  placeholder: string;
  value: string;
  onApply: () => void;
  onChange: (value: string) => void;
}) {
  return (
    <Space orientation="vertical" size={12} style={{ width: "100%" }}>
      <Input.TextArea
        className="source-input"
        onChange={(event: ChangeEvent<HTMLTextAreaElement>) =>
          props.onChange(event.target.value)
        }
        placeholder={props.placeholder}
        value={props.value}
      />
      <Flex justify="space-between" gap="small">
        <Text type="secondary">编辑期间不会覆盖其他区域，点击应用后统一同步。</Text>
        <Space>
          <Button
            aria-label="Copy input"
            icon={<CopyOutlined />}
            onClick={() => void copyText(props.value).then(reportCopyResult)}
          />
          <Button icon={<ImportOutlined />} onClick={props.onApply} type="primary">
            {props.actionLabel}
          </Button>
        </Space>
      </Flex>
    </Space>
  );
}

function buildPayload(input: EditorState): DirectivePayload {
  const ops = input.headerOps
    .map((item) => ({
      op: item.op,
      name: item.name.trim(),
      values:
        item.op === "-"
          ? []
          : item.values.split(",").map((value) => value.trim()).filter(Boolean),
    }))
    .filter((item) => item.name);

  const payload: DirectivePayload = { target: { url: input.targetURL.trim() } };
  if (!input.joinPath) payload.target.join_path = false;
  if (input.proxyURL.trim()) payload.proxy = input.proxyURL.trim();
  if (ops.length > 0) payload.headers = { mode: input.headerMode, ops };
  return payload;
}

function payloadToEditor(payload: DirectivePayload): EditorState {
  return {
    targetURL: payload.target.url,
    joinPath: payload.target.join_path ?? true,
    proxyURL: payload.proxy ?? "",
    headerMode: payload.headers?.mode ?? "patch",
    headerOps: (payload.headers?.ops ?? []).map((item) =>
      newHeaderOp(item.op, item.name, (item.values ?? []).join(", ")),
    ),
  };
}

function parsePayloadJSON(value: string): DirectivePayload {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    throw new Error("Payload 不是有效的 JSON");
  }
  return validatePayload(parsed);
}

function validatePayload(value: unknown): DirectivePayload {
  if (!isRecord(value)) throw new Error("Payload 必须是 JSON object");
  assertKnownKeys(value, ["target", "proxy", "headers"], "Payload");
  if (!isRecord(value.target)) throw new Error("target 必须是 object");
  assertKnownKeys(value.target, ["url", "join_path"], "target");
  if (typeof value.target.url !== "string" || !value.target.url.trim()) {
    throw new Error("target.url 必须是非空字符串");
  }
  if (value.target.join_path !== undefined && typeof value.target.join_path !== "boolean") {
    throw new Error("target.join_path 必须是 boolean");
  }
  if (value.proxy !== undefined && typeof value.proxy !== "string") {
    throw new Error("proxy 必须是 string");
  }

  let headers: DirectivePayload["headers"];
  if (value.headers !== undefined) {
    if (!isRecord(value.headers)) throw new Error("headers 必须是 object");
    assertKnownKeys(value.headers, ["mode", "ops"], "headers");
    if (value.headers.mode !== undefined && !["patch", "replace"].includes(String(value.headers.mode))) {
      throw new Error("headers.mode 只能是 patch 或 replace");
    }
    if (value.headers.ops !== undefined && !Array.isArray(value.headers.ops)) {
      throw new Error("headers.ops 必须是 array");
    }
    const ops = (value.headers.ops ?? []).map((item, index) => validateHeaderOp(item, index));
    headers = { mode: value.headers.mode as "patch" | "replace" | undefined, ops };
  }

  return {
    target: {
      url: value.target.url.trim(),
      ...(value.target.join_path === false ? { join_path: false } : {}),
    },
    ...(value.proxy?.trim() ? { proxy: value.proxy.trim() } : {}),
    ...(headers ? { headers } : {}),
  };
}

function validateHeaderOp(value: unknown, index: number) {
  if (!isRecord(value)) throw new Error(`headers.ops[${index}] 必须是 object`);
  assertKnownKeys(value, ["op", "name", "values"], `headers.ops[${index}]`);
  if (!["=", "+", "-"].includes(String(value.op))) {
    throw new Error(`headers.ops[${index}].op 只能是 =、+ 或 -`);
  }
  if (typeof value.name !== "string" || !value.name.trim()) {
    throw new Error(`headers.ops[${index}].name 必须是非空字符串`);
  }
  if (value.values !== undefined &&
      (!Array.isArray(value.values) || value.values.some((item) => typeof item !== "string"))) {
    throw new Error(`headers.ops[${index}].values 必须是 string array`);
  }
  return {
    op: value.op as HeaderOp["op"],
    name: value.name.trim(),
    ...((value.values as string[] | undefined)?.length ? { values: value.values as string[] } : {}),
  };
}

function decodeToken(value: string): DirectivePayload {
  const token = normalizeToken(value);
  if (!token.startsWith(tokenPrefix)) throw new Error("Token 必须以 dproxy.10. 开头");
  const raw = token.slice(tokenPrefix.length);
  if (!raw) throw new Error("Token 缺少 payload");
  try {
    const json = new TextDecoder().decode(base64URLDecode(raw));
    return parsePayloadJSON(json);
  } catch (err) {
    throw new Error(errorMessage(err, "Token payload 解码失败"));
  }
}

function normalizeToken(value: string) {
  let token = value.trim();
  if (token.toLowerCase().startsWith("authorization:")) {
    token = token.slice("authorization:".length).trim();
  }
  if (token.toLowerCase().startsWith("bearer ")) {
    token = token.slice("bearer ".length).trim();
  }
  return token;
}

function encodeToken(payload: DirectivePayload) {
  return `${tokenPrefix}${base64URL(JSON.stringify(payload))}`;
}

function formatPayload(payload: DirectivePayload) {
  return JSON.stringify(payload, null, 2);
}

function base64URL(value: string) {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}

function base64URLDecode(value: string) {
  const normalized = value.replaceAll("-", "+").replaceAll("_", "/");
  const padded = normalized.padEnd(normalized.length + ((4 - normalized.length % 4) % 4), "=");
  const binary = atob(padded);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function assertKnownKeys(value: Record<string, unknown>, keys: string[], label: string) {
  const unknown = Object.keys(value).find((key) => !keys.includes(key));
  if (unknown) throw new Error(`${label} 包含未知字段 ${unknown}`);
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

function newHeaderOp(op: HeaderOp["op"], name: string, values: string): HeaderOp {
  headerOpID += 1;
  return { key: `header-op-${headerOpID}`, op, name, values };
}

function reportCopyResult(ok: boolean) {
  void (ok ? message.success("已复制") : message.error("复制失败"));
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
  document.body.appendChild(textarea);
  textarea.select();
  try {
    return document.execCommand("copy");
  } finally {
    document.body.removeChild(textarea);
  }
}
