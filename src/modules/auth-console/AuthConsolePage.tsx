import { CopyOutlined, PlusOutlined, DeleteOutlined } from "@ant-design/icons";
import { WorkbenchPage } from "@lwmacct/260627-antd-workbench";
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Col,
  Flex,
  Form,
  Input,
  Row,
  Select,
  Space,
  Table,
  Typography,
  message,
} from "antd";
import type { TableColumnsType } from "antd";
import { useMemo, useState } from "react";

const { Text, Paragraph } = Typography;
const tokenPrefix = "dproxy.10.";

type HeaderOp = {
  key: string;
  op: "=" | "+" | "-";
  name: string;
  values: string;
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
      op: string;
      name: string;
      values?: string[];
    }>;
  };
};

let headerOpID = 0;

const defaultHeaderOps: HeaderOp[] = [
  {
    key: newHeaderOpKey(),
    op: "=",
    name: "Authorization",
    values: "Bearer upstream-token",
  },
  {
    key: newHeaderOpKey(),
    op: "=",
    name: "X-Tenant",
    values: "tenant-a",
  },
];

export function AuthConsolePage() {
  const [targetURL, setTargetURL] = useState("https://httpbin.org/anything");
  const [joinPath, setJoinPath] = useState(true);
  const [proxyURL, setProxyURL] = useState("");
  const [headerMode, setHeaderMode] = useState<"patch" | "replace">("patch");
  const [headerOps, setHeaderOps] = useState<HeaderOp[]>(defaultHeaderOps);
  const [error, setError] = useState<string | null>(null);

  const payload = useMemo(
    () =>
      buildPayload({
        headerMode,
        headerOps,
        joinPath,
        proxyURL,
        targetURL,
      }),
    [headerMode, headerOps, joinPath, proxyURL, targetURL],
  );

  const payloadJSON = useMemo(() => JSON.stringify(payload, null, 2), [payload]);
  const token = useMemo(() => {
    setError(null);
    try {
      return `${tokenPrefix}${base64URL(JSON.stringify(payload))}`;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Encode failed");
      return "";
    }
  }, [payload]);
  const authorization = token ? `Authorization: Bearer ${token}` : "";
  const curlSample = [
    "curl -i 'http://127.0.0.1:23197/v1/chat/completions' \\",
    `  -H '${authorization}' \\`,
    "  -H 'Content-Type: application/json' \\",
    "  --data '{\"message\":\"hello through directive proxy\"}'",
  ].join("\n");

  const columns: TableColumnsType<HeaderOp> = [
    {
      title: "Op",
      dataIndex: "op",
      width: 96,
      render: (_, record) => (
        <Select
          options={[
            { label: "Set", value: "=" },
            { label: "Add", value: "+" },
            { label: "Remove", value: "-" },
          ]}
          value={record.op}
          onChange={(value) => updateHeaderOp(record.key, { op: value })}
        />
      ),
    },
    {
      title: "Name",
      dataIndex: "name",
      render: (_, record) => (
        <Input
          value={record.name}
          onChange={(event) =>
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
          placeholder="comma separated"
          value={record.values}
          onChange={(event) =>
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
          onClick={() => setHeaderOps((items) => items.filter((item) => item.key !== record.key))}
          type="text"
        />
      ),
    },
  ];

  function updateHeaderOp(key: string, patch: Partial<HeaderOp>) {
    setHeaderOps((items) =>
      items.map((item) => (item.key === key ? { ...item, ...patch } : item)),
    );
  }

  return (
    <WorkbenchPage
      description="生成可用于 data plane 的 Authorization header。"
      extra={
        <Button
          icon={<CopyOutlined />}
          onClick={() => void copyText(authorization).then(reportCopyResult)}
          type="primary"
        >
          Copy Authorization
        </Button>
      }
      title="Authorization 生成器"
    >
      {error ? <Alert className="status-alert" message={error} type="error" /> : null}

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={13}>
          <Card size="small">
          <Form layout="vertical">
            <Form.Item label="Target URL">
              <Input value={targetURL} onChange={(event) => setTargetURL(event.target.value)} />
            </Form.Item>
            <Flex gap="small" wrap>
              <Form.Item label="Join Path">
                <Checkbox checked={joinPath} onChange={(event) => setJoinPath(event.target.checked)}>
                  enabled
                </Checkbox>
              </Form.Item>
              <Form.Item className="grow-field" label="Proxy">
                <Input
                  allowClear
                  placeholder="socks5://user:pass@127.0.0.1:1080"
                  value={proxyURL}
                  onChange={(event) => setProxyURL(event.target.value)}
                />
              </Form.Item>
              <Form.Item label="Header Mode">
                <Select
                  options={[
                    { label: "Patch", value: "patch" },
                    { label: "Replace", value: "replace" },
                  ]}
                  value={headerMode}
                  onChange={setHeaderMode}
                />
              </Form.Item>
            </Flex>
          </Form>

          <Flex align="center" justify="space-between" style={{ marginBottom: 12 }}>
            <Text strong>Header Ops</Text>
            <Button
              icon={<PlusOutlined />}
              onClick={() =>
                setHeaderOps((items) => [
                  ...items,
                  { key: newHeaderOpKey(), op: "=", name: "", values: "" },
                ])
              }
            >
              Add
            </Button>
          </Flex>
          <Table<HeaderOp>
            columns={columns}
            dataSource={headerOps}
            pagination={false}
            rowKey="key"
            size="small"
          />
          </Card>
        </Col>

        <Col xs={24} xl={11}>
          <Space direction="vertical" size={14} style={{ width: "100%" }}>
          <OutputBlock title="Payload JSON" value={payloadJSON} />
          <CopyOnlyBlock title="Token" value={token} />
          <CopyOnlyBlock title="Authorization Header" value={authorization} />
          <CopyOnlyBlock title="curl Sample" value={curlSample} />
          </Space>
        </Col>
      </Row>
    </WorkbenchPage>
  );
}

function OutputBlock({ title, value }: { title: string; value: string }) {
  return (
    <Card
      extra={
        <Button
          aria-label={`Copy ${title}`}
          icon={<CopyOutlined />}
          onClick={() => void copyText(value).then(reportCopyResult)}
          size="small"
          type="text"
        />
      }
      size="small"
      title={title}
    >
      <Paragraph className="code-output">
        {value}
      </Paragraph>
    </Card>
  );
}

function CopyOnlyBlock({ title, value }: { title: string; value: string }) {
  return (
    <Card
      extra={
        <Button
          aria-label={`Copy ${title}`}
          icon={<CopyOutlined />}
          onClick={() => void copyText(value).then(reportCopyResult)}
          size="small"
          type="text"
        />
      }
      size="small"
      title={title}
    />
  );
}

function buildPayload(input: {
  headerMode: "patch" | "replace";
  headerOps: HeaderOp[];
  joinPath: boolean;
  proxyURL: string;
  targetURL: string;
}): DirectivePayload {
  const ops = input.headerOps
    .map((item) => ({
      op: item.op,
      name: item.name.trim(),
      values: item.values
        .split(",")
        .map((value) => value.trim())
        .filter(Boolean),
    }))
    .filter((item) => item.name);

  const payload: DirectivePayload = {
    target: {
      url: input.targetURL.trim(),
    },
  };
  if (!input.joinPath) {
    payload.target.join_path = false;
  }
  if (input.proxyURL.trim()) {
    payload.proxy = input.proxyURL.trim();
  }
  if (ops.length > 0) {
    payload.headers = {
      mode: input.headerMode,
      ops,
    };
  }
  return payload;
}

function base64URL(value: string) {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
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

function newHeaderOpKey() {
  headerOpID += 1;
  return `header-op-${Date.now()}-${headerOpID}`;
}
