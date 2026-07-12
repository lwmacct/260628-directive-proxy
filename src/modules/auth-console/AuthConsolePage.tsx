import {
  CopyOutlined,
  DeleteOutlined,
  ImportOutlined,
  PlusOutlined,
  SendOutlined,
} from "@ant-design/icons";
import {
  WorkbenchPage,
  WorkbenchPanel,
} from "@lwmacct/260627-antd-workbench";
import {
  Alert,
  App as AntdApp,
  Button,
  Checkbox,
  Col,
  Flex,
  Form,
  Input,
  Row,
  Segmented,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from "antd";
import type { TableColumnsType } from "antd";
import type { CheckboxChangeEvent } from "antd/es/checkbox";
import { useMemo, useRef, useState, type ChangeEvent } from "react";
import { useText, type Text as AppText } from "../../shared/i18n";

const { Text } = Typography;
const tokenFamily = "dproxy";
const tokenVersion = "13";

type DirectiveSource = "inline" | "http" | "redis";

type ResolverHeader = {
  key: string;
  name: string;
  value: string;
};

type HeaderOp = {
  key: string;
  op: "=" | "+" | "-";
  selector: "name" | "glob" | "preset";
  pattern: string;
  values: string[];
};

type EditorState = {
  source: DirectiveSource;
  remoteKey: string;
  httpURL: string;
  redisURL: string;
  resolverHeaders: ResolverHeader[];
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
      name?: string;
      glob?: string;
      preset?: "proxy-disclosure";
      values?: string[];
    }>;
  };
};

type DirectiveHeaderOp = NonNullable<NonNullable<DirectivePayload["headers"]>["ops"]>[number];

type DecodedDirectiveToken =
  | { source: "inline"; payload: DirectivePayload }
  | { source: "http" | "redis"; spec: RemoteSpec };

type RemoteSpec = {
  type: "http" | "redis";
  url: string;
  key?: string;
  headers?: Record<string, string>;
};

type RequestResult = {
  body: string;
  duration: number;
  headers: string;
  status: number;
  statusText: string;
};

let headerOpID = 0;
let resolverHeaderID = 0;

const initialEditor: EditorState = {
  source: "inline",
  remoteKey: "team-a/openai",
  httpURL: "https://policy.example.com/v1/resolve",
  redisURL: "rediss://user:password@redis.example.com:6380/1",
  resolverHeaders: [newResolverHeader("Authorization", "Bearer policy-token")],
  targetURL: "https://httpbin.org/anything",
  joinPath: true,
  proxyURL: "",
  headerMode: "patch",
  headerOps: [
    newHeaderOp("-", "preset", "proxy-disclosure", []),
    newHeaderOp("=", "name", "Authorization", ["Bearer upstream-token"]),
    newHeaderOp("=", "name", "X-Dproxy-Key", ["dproxy-demo-key"]),
  ],
};

export function AuthConsolePage() {
  const t = useText();
  const { message } = AntdApp.useApp();
  const [editor, setEditor] = useState(initialEditor);
  const initialPayload = useMemo(() => buildPayload(initialEditor), []);
  const [payloadInput, setPayloadInput] = useState(() => formatPayload(initialPayload));
  const [tokenInput, setTokenInput] = useState(() => encodeDirectiveToken(initialEditor, initialPayload));
  const [activeSource, setActiveSource] = useState<"payload" | "token">("payload");
  const [error, setError] = useState<string | null>(null);
  const [requestMethod, setRequestMethod] = useState("POST");
  const [requestPath, setRequestPath] = useState("/v1/chat/completions");
  const [requestHeaders, setRequestHeaders] = useState(
    '{\n  "Content-Type": "application/json"\n}',
  );
  const [requestBody, setRequestBody] = useState(
    '{\n  "model": "example-model",\n  "messages": [\n    { "role": "user", "content": "Hello" }\n  ]\n}',
  );
  const [requestLoading, setRequestLoading] = useState(false);
  const [requestError, setRequestError] = useState<string | null>(null);
  const [requestResult, setRequestResult] = useState<RequestResult | null>(null);
  const requestController = useRef<AbortController | null>(null);

  const payload = useMemo(() => buildPayload(editor), [editor]);
  const directiveToken = encodeDirectiveToken(editor, payload);
  const sourceDirty = activeSource === "payload"
    ? payloadInput !== formatPayload(payload)
    : tokenInput !== directiveToken;

  function updateEditor(patch: Partial<EditorState>) {
    const next = { ...editor, ...patch };
    setEditor(next);
    syncInputs(next, buildPayload(next));
  }

  function syncInputs(nextEditor: EditorState, nextPayload: DirectivePayload) {
    setPayloadInput(formatPayload(nextPayload));
    setTokenInput(encodeDirectiveToken(nextEditor, nextPayload));
    setError(null);
  }

  function updateDirectiveSource(source: DirectiveSource) {
    setActiveSource(source === "inline" ? "payload" : "token");
    updateEditor({ source });
  }

  function applyPayloadInput() {
    try {
      applyPayload(parsePayloadJSON(payloadInput, t.authConsole));
      void message.success(t.authConsole.payloadApplied);
    } catch (err) {
      setError(errorMessage(err, t.authConsole.payloadParseFailed));
    }
  }

  function applyTokenInput() {
    try {
      const decoded = decodeDirectiveToken(tokenInput, t.authConsole);
      if (decoded.source === "inline") {
        applyPayload(decoded.payload);
      } else {
        const next = remoteSpecToEditor(editor, decoded.spec);
        setEditor(next);
        setTokenInput(encodeDirectiveToken(next, payload));
        setActiveSource("token");
        setError(null);
      }
      void message.success(t.authConsole.tokenApplied);
    } catch (err) {
      setError(errorMessage(err, t.authConsole.tokenParseFailed));
    }
  }

  function applyPayload(nextPayload: DirectivePayload) {
    const next = { ...editor, ...payloadToEditor(nextPayload), source: "inline" as const };
    setEditor(next);
    syncInputs(next, nextPayload);
  }

  function updateHeaderOp(key: string, patch: Partial<HeaderOp>) {
    updateEditor({
      headerOps: editor.headerOps.map((item) =>
        item.key === key ? { ...item, ...patch } : item,
      ),
    });
  }

  async function sendRequest() {
    try {
      if (editor.source !== "inline") validateRemoteSpec(buildRemoteSpec(editor), t.authConsole);
      const path = normalizeRequestPath(requestPath, t.authConsole);
      const headers = parseRequestHeaders(requestHeaders, t.authConsole);
      const controller = new AbortController();
      requestController.current?.abort();
      requestController.current = controller;
      setRequestLoading(true);
      setRequestError(null);
      setRequestResult(null);
      const startedAt = performance.now();
      const response = await fetch(path, {
        method: requestMethod,
        headers: { ...headers, Authorization: `Bearer ${directiveToken}` },
        body: requestMethod === "GET" || requestMethod === "HEAD" ? undefined : requestBody,
        signal: controller.signal,
      });
      const body = await response.text();
      setRequestResult({
        body,
        duration: Math.round(performance.now() - startedAt),
        headers: formatResponseHeaders(response.headers),
        status: response.status,
        statusText: response.statusText,
      });
    } catch (err) {
      setRequestError(
        err instanceof DOMException && err.name === "AbortError"
          ? t.authConsole.requestCancelled
          : errorMessage(err, t.authConsole.requestFailed),
      );
    } finally {
      requestController.current = null;
      setRequestLoading(false);
    }
  }

  const columns: TableColumnsType<HeaderOp> = [
    {
      title: t.authConsole.op,
      dataIndex: "op",
      width: 104,
      render: (_, record) => (
        <Select
          disabled={record.selector === "preset"}
          options={record.selector === "preset" ? [
            { label: t.authConsole.remove, value: "-" },
          ] : [
            { label: t.authConsole.set, value: "=" },
            { label: "Add", value: "+" },
            { label: t.authConsole.remove, value: "-" },
          ]}
          value={record.op}
          onChange={(op: HeaderOp["op"]) => updateHeaderOp(record.key, { op })}
        />
      ),
    },
    {
      title: t.authConsole.match,
      dataIndex: "selector",
      width: 220,
      render: (_, record) => (
        <Segmented
          options={[
            { label: t.authConsole.exact, value: "name" },
            { label: "Glob", value: "glob" },
            { label: t.authConsole.preset, value: "preset" },
          ]}
          value={record.selector}
          onChange={(selector: HeaderOp["selector"]) =>
            updateHeaderOp(record.key, selector === "preset"
              ? { selector, op: "-", pattern: "proxy-disclosure", values: [] }
              : { selector, pattern: "" })
          }
        />
      ),
    },
    {
      title: t.authConsole.selector,
      dataIndex: "pattern",
      render: (_, record) => record.selector === "preset" ? (
        <Select
          options={[{ label: "proxy-disclosure", value: "proxy-disclosure" }]}
          style={{ width: "100%" }}
          value={record.pattern}
          onChange={(pattern: string) => updateHeaderOp(record.key, { pattern })}
        />
      ) : (
        <Input
          placeholder={record.selector === "glob" ? "X-Tenant-*" : "Authorization"}
          value={record.pattern}
          onChange={(event: ChangeEvent<HTMLInputElement>) =>
            updateHeaderOp(record.key, { pattern: event.target.value })
          }
        />
      ),
    },
    {
      title: t.authConsole.values,
      dataIndex: "values",
      render: (_, record) => record.op === "-" ? null : (
        <Select
          mode="tags"
          open={false}
          placeholder={t.authConsole.valuePlaceholder}
          style={{ width: "100%" }}
          value={record.values}
          onChange={(values: string[]) => updateHeaderOp(record.key, { values })}
        />
      ),
    },
    {
      title: "",
      key: "actions",
      width: 56,
      render: (_, record) => (
        <Button
          aria-label={t.authConsole.removeHeaderOp}
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
      description={t.authConsole.description}
      title={t.app.authConsole}
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
          <WorkbenchPanel title={t.authConsole.structured}>
            <Form layout="vertical">
              <Form.Item label={t.authConsole.directiveSource}>
                <Segmented
                  options={[
                    { label: "Inline", value: "inline" },
                    { label: "HTTP API", value: "http" },
                    { label: "Redis", value: "redis" },
                  ]}
                  value={editor.source}
                  onChange={(source: DirectiveSource) => updateDirectiveSource(source)}
                />
              </Form.Item>
              {editor.source === "inline" ? (
                <>
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
                        {t.authConsole.enabled}
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
                </>
              ) : (
                <>
                  <Form.Item label={editor.source === "http" ? t.authConsole.httpResolverURL : t.authConsole.redisURL}>
                    <Input
                      placeholder={editor.source === "http"
                        ? "https://policy.example.com/v1/resolve"
                        : "rediss://user:password@redis.example.com:6380/1"}
                      value={editor.source === "http" ? editor.httpURL : editor.redisURL}
                      onChange={(event: ChangeEvent<HTMLInputElement>) =>
                        updateEditor(editor.source === "http"
                          ? { httpURL: event.target.value }
                          : { redisURL: event.target.value })
                      }
                    />
                  </Form.Item>
                  <Form.Item label={editor.source === "http" ? t.authConsole.optionalRemoteKey : t.authConsole.redisKey}>
                    <Input
                      placeholder="team-a/openai"
                      status={editor.source === "redis" && !isRemoteKeyValid(editor.remoteKey) ? "error" : undefined}
                      value={editor.remoteKey}
                      onChange={(event: ChangeEvent<HTMLInputElement>) =>
                        updateEditor({ remoteKey: event.target.value })
                      }
                    />
                  </Form.Item>
                  {editor.source === "http" ? (
                    <Form.Item label={t.authConsole.resolverHeaders}>
                      <Flex gap="small" vertical>
                        {editor.resolverHeaders.map((header) => (
                          <Flex gap="small" key={header.key} wrap>
                            <Input
                              placeholder="Authorization"
                              style={{ flex: "1 1 160px", minWidth: 0 }}
                              value={header.name}
                              onChange={(event: ChangeEvent<HTMLInputElement>) =>
                                updateEditor({
                                  resolverHeaders: editor.resolverHeaders.map((item) => item.key === header.key
                                    ? { ...item, name: event.target.value }
                                    : item),
                                })
                              }
                            />
                            <Input
                              placeholder="Bearer policy-token"
                              style={{ flex: "1 1 160px", minWidth: 0 }}
                              value={header.value}
                              onChange={(event: ChangeEvent<HTMLInputElement>) =>
                                updateEditor({
                                  resolverHeaders: editor.resolverHeaders.map((item) => item.key === header.key
                                    ? { ...item, value: event.target.value }
                                    : item),
                                })
                              }
                            />
                            <Button
                              aria-label={t.authConsole.removeResolverHeader}
                              icon={<DeleteOutlined />}
                              onClick={() => updateEditor({
                                resolverHeaders: editor.resolverHeaders.filter((item) => item.key !== header.key),
                              })}
                            />
                          </Flex>
                        ))}
                        <Button
                          icon={<PlusOutlined />}
                          onClick={() => updateEditor({
                            resolverHeaders: [...editor.resolverHeaders, newResolverHeader("", "")],
                          })}
                        >
                          {t.authConsole.addResolverHeader}
                        </Button>
                      </Flex>
                    </Form.Item>
                  ) : null}
                </>
              )}
            </Form>

            {editor.source === "inline" ? (
              <>
                <Flex align="center" justify="space-between" style={{ marginBottom: 12 }}>
                  <Text strong>{t.authConsole.headerOps}</Text>
                  <Button
                    icon={<PlusOutlined />}
                    onClick={() =>
                      updateEditor({
                        headerOps: [...editor.headerOps, newHeaderOp("=", "name", "", [])],
                      })
                    }
                  >
                    {t.authConsole.add}
                  </Button>
                </Flex>
                <Table<HeaderOp>
                  columns={columns}
                  dataSource={editor.headerOps}
                  pagination={false}
                  rowKey="key"
                  scroll={{ x: 920 }}
                  size="small"
                />
              </>
            ) : null}
          </WorkbenchPanel>
        </Col>

        <Col xs={24} xl={11}>
          <WorkbenchPanel title={t.authConsole.editableSources}>
            <Tabs
              activeKey={activeSource}
              items={editor.source === "inline" ? [
                {
                  key: "payload",
                  label: "Payload JSON",
                  children: (
                    <SourceEditor
                      placeholder='{ "target": { "url": "https://api.example.com" } }'
                      value={payloadInput}
                      onChange={setPayloadInput}
                    />
                  ),
                },
                {
                  key: "token",
                  label: "Token",
                  children: (
                    <SourceEditor
                      placeholder="dproxy.13.i..."
                      value={tokenInput}
                      onChange={setTokenInput}
                    />
                  ),
                },
              ] : [
                {
                  key: "token",
                  label: "Token",
                  children: (
                    <SourceEditor
                      placeholder="dproxy.13.r..."
                      value={tokenInput}
                      onChange={setTokenInput}
                    />
                  ),
                },
              ]}
              onChange={(key: string) => setActiveSource(key as "payload" | "token")}
            />
            <Flex align="center" gap="small" justify="space-between" wrap>
              <Tag color={sourceDirty ? "orange" : "green"}>
                {sourceDirty ? t.authConsole.dirty : t.authConsole.synced}
              </Tag>
              <Space wrap>
                <Button
                  icon={<CopyOutlined />}
                  onClick={() =>
                    void copyText(
                      activeSource === "payload"
                        ? payloadInput
                        : tokenInput,
                    ).then((ok) => {
                      void (ok
                        ? message.success(t.authConsole.copied)
                        : message.error(t.authConsole.copyFailed));
                    })
                  }
                >
                  {activeSource === "payload" ? t.authConsole.copyPayload : t.authConsole.copyToken}
                </Button>
                <Button
                  icon={<ImportOutlined />}
                  onClick={activeSource === "payload" ? applyPayloadInput : applyTokenInput}
                  type="primary"
                >
                  {activeSource === "payload" ? t.authConsole.applyPayload : t.authConsole.parseToken}
                </Button>
              </Space>
            </Flex>
          </WorkbenchPanel>
        </Col>
      </Row>

      <RequestPanel
        body={requestBody}
        error={requestError}
        headers={requestHeaders}
        loading={requestLoading}
        method={requestMethod}
        path={requestPath}
        result={requestResult}
        text={t.authConsole}
        onBodyChange={setRequestBody}
        onCancel={() => requestController.current?.abort()}
        onHeadersChange={setRequestHeaders}
        onMethodChange={setRequestMethod}
        onPathChange={setRequestPath}
        onSend={() => void sendRequest()}
      />
    </WorkbenchPage>
  );
}

function RequestPanel(props: {
  body: string;
  error: string | null;
  headers: string;
  loading: boolean;
  method: string;
  path: string;
  result: RequestResult | null;
  text: AppText["authConsole"];
  onBodyChange: (value: string) => void;
  onCancel: () => void;
  onHeadersChange: (value: string) => void;
  onMethodChange: (value: string) => void;
  onPathChange: (value: string) => void;
  onSend: () => void;
}) {
  const bodyDisabled = props.method === "GET" || props.method === "HEAD";
  return (
    <WorkbenchPanel
      extra={
        <Space>
          {props.loading ? <Button onClick={props.onCancel}>{props.text.cancel}</Button> : null}
          <Button
            icon={<SendOutlined />}
            loading={props.loading}
            onClick={props.onSend}
            type="primary"
          >
            {props.text.send}
          </Button>
        </Space>
      }
      style={{ marginTop: 16 }}
      title={props.text.requestDebug}
    >
      <Text type="secondary">
        {props.text.requestDescription}
      </Text>
      <Flex gap="small" style={{ marginTop: 12 }} wrap>
        <Select
          className="request-method"
          options={["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"].map((value) => ({
            label: value,
            value,
          }))}
          value={props.method}
          onChange={props.onMethodChange}
        />
        <Input
          className="request-path"
          placeholder="/v1/chat/completions"
          value={props.path}
          onChange={(event: ChangeEvent<HTMLInputElement>) =>
            props.onPathChange(event.target.value)
          }
        />
      </Flex>

      <Row gutter={[16, 16]} style={{ marginTop: 4 }}>
        <Col xs={24} lg={12}>
          <Form layout="vertical">
            <Form.Item label={props.text.requestHeaders}>
              <Input.TextArea
                autoSize={{ minRows: 4, maxRows: 10 }}
                className="request-code-input"
                value={props.headers}
                onChange={(event: ChangeEvent<HTMLTextAreaElement>) =>
                  props.onHeadersChange(event.target.value)
                }
              />
            </Form.Item>
            <Form.Item
              help={bodyDisabled ? props.text.bodyDisabled(props.method) : undefined}
              label={props.text.requestBody}
            >
              <Input.TextArea
                autoSize={{ minRows: 9, maxRows: 20 }}
                className="request-code-input"
                disabled={bodyDisabled}
                value={props.body}
                onChange={(event: ChangeEvent<HTMLTextAreaElement>) =>
                  props.onBodyChange(event.target.value)
                }
              />
            </Form.Item>
          </Form>
        </Col>
        <Col xs={24} lg={12}>
          {props.error ? <Alert showIcon title={props.error} type="error" /> : null}
          {!props.error && !props.result ? (
            <Alert showIcon title={props.text.waiting} type="info" />
          ) : null}
          {props.result ? (
            <Space orientation="vertical" size={12} style={{ width: "100%" }}>
              <Flex align="center" gap="small">
                <Tag color={statusColor(props.result.status)}>
                  {props.result.status} {props.result.statusText}
                </Tag>
                <Text type="secondary">{props.result.duration} ms</Text>
              </Flex>
              <Form layout="vertical">
                <Form.Item label={props.text.responseHeaders}>
                  <Input.TextArea
                    autoSize={{ minRows: 4, maxRows: 10 }}
                    className="request-code-input"
                    readOnly
                    value={props.result.headers}
                  />
                </Form.Item>
                <Form.Item label={props.text.responseBody}>
                  <Input.TextArea
                    autoSize={{ minRows: 9, maxRows: 20 }}
                    className="request-code-input"
                    readOnly
                    value={props.result.body}
                  />
                </Form.Item>
              </Form>
            </Space>
          ) : null}
        </Col>
      </Row>
    </WorkbenchPanel>
  );
}

function SourceEditor(props: {
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <Input.TextArea
      autoSize={{ minRows: 10 }}
      className="source-input"
      onChange={(event: ChangeEvent<HTMLTextAreaElement>) =>
        props.onChange(event.target.value)
      }
      placeholder={props.placeholder}
      value={props.value}
    />
  );
}

function buildPayload(input: EditorState): DirectivePayload {
  const ops = input.headerOps.flatMap<DirectiveHeaderOp>((item) => {
    const pattern = item.pattern.trim();
    if (!pattern) return [];
    const selector = item.selector === "name"
      ? { name: pattern }
      : item.selector === "glob"
        ? { glob: pattern }
        : { preset: pattern as "proxy-disclosure" };
    return [{
      op: item.op,
      ...selector,
      ...(item.op === "-" ? {} : { values: item.values }),
    }];
  });

  const payload: DirectivePayload = { target: { url: input.targetURL.trim() } };
  if (!input.joinPath) payload.target.join_path = false;
  if (input.proxyURL.trim()) payload.proxy = input.proxyURL.trim();
  if (ops.length > 0) payload.headers = { mode: input.headerMode, ops };
  return payload;
}

function payloadToEditor(payload: DirectivePayload): Pick<EditorState, "targetURL" | "joinPath" | "proxyURL" | "headerMode" | "headerOps"> {
  return {
    targetURL: payload.target.url,
    joinPath: payload.target.join_path ?? true,
    proxyURL: payload.proxy ?? "",
    headerMode: payload.headers?.mode ?? "patch",
    headerOps: (payload.headers?.ops ?? []).map((item) =>
      newHeaderOp(
        item.op,
        item.preset !== undefined ? "preset" : item.glob !== undefined ? "glob" : "name",
        item.name ?? item.glob ?? item.preset ?? "",
        item.values ?? [],
      ),
    ),
  };
}

function parsePayloadJSON(value: string, text: AppText["authConsole"]): DirectivePayload {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    throw new Error(text.invalidJSON("Payload"));
  }
  return validatePayload(parsed, text);
}

function validatePayload(value: unknown, text: AppText["authConsole"]): DirectivePayload {
  if (!isRecord(value)) throw new Error(text.mustBe("Payload", "JSON object"));
  assertKnownKeys(value, ["target", "proxy", "headers"], "Payload", text);
  if (!isRecord(value.target)) throw new Error(text.mustBe("target", "object"));
  assertKnownKeys(value.target, ["url", "join_path"], "target", text);
  if (typeof value.target.url !== "string" || !value.target.url.trim()) {
    throw new Error(text.nonEmptyString("target.url"));
  }
  if (value.target.join_path !== undefined && typeof value.target.join_path !== "boolean") {
    throw new Error(text.mustBe("target.join_path", "boolean"));
  }
  if (value.proxy !== undefined && typeof value.proxy !== "string") {
    throw new Error(text.mustBe("proxy", "string"));
  }

  let headers: DirectivePayload["headers"];
  if (value.headers !== undefined) {
    if (!isRecord(value.headers)) throw new Error(text.mustBe("headers", "object"));
    assertKnownKeys(value.headers, ["mode", "ops"], "headers", text);
    if (value.headers.mode !== undefined && !["patch", "replace"].includes(String(value.headers.mode))) {
      throw new Error(text.onlyValues("headers.mode", "patch or replace"));
    }
    if (value.headers.ops !== undefined && !Array.isArray(value.headers.ops)) {
      throw new Error(text.mustBe("headers.ops", "array"));
    }
    const ops = (value.headers.ops ?? []).map((item, index) => validateHeaderOp(item, index, text));
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

function validateHeaderOp(value: unknown, index: number, text: AppText["authConsole"]) {
  const label = `headers.ops[${index}]`;
  if (!isRecord(value)) throw new Error(text.mustBe(label, "object"));
  assertKnownKeys(value, ["op", "name", "glob", "preset", "values"], label, text);
  if (!["=", "+", "-"].includes(String(value.op))) {
    throw new Error(text.onlyValues(`${label}.op`, "=, +, or -"));
  }
  if (value.name !== undefined && typeof value.name !== "string") {
    throw new Error(text.mustBe(`${label}.name`, "string"));
  }
  if (value.glob !== undefined && typeof value.glob !== "string") {
    throw new Error(text.mustBe(`${label}.glob`, "string"));
  }
  if (value.preset !== undefined && typeof value.preset !== "string") {
    throw new Error(text.mustBe(`${label}.preset`, "string"));
  }
  const hasName = typeof value.name === "string" && Boolean(value.name.trim());
  const hasGlob = typeof value.glob === "string" && Boolean(value.glob.trim());
  const hasPreset = typeof value.preset === "string" && Boolean(value.preset.trim());
  if ([hasName, hasGlob, hasPreset].filter(Boolean).length !== 1) {
    throw new Error(text.exactlyOneSelector(label));
  }
  if (hasName && !isValidHeaderName((value.name as string).trim())) {
    throw new Error(text.invalidHeaderName(`${label}.name`));
  }
  if (hasGlob) {
    assertValidGlob((value.glob as string).trim(), `${label}.glob`, text);
  }
  if (hasPreset && value.preset !== "proxy-disclosure") {
    throw new Error(text.onlyValues(`${label}.preset`, "proxy-disclosure"));
  }
  if (value.values !== undefined &&
      (!Array.isArray(value.values) || value.values.some((item) => typeof item !== "string"))) {
    throw new Error(text.mustBe(`${label}.values`, "string array"));
  }
  const values = value.values as string[] | undefined;
  if (value.op === "-" && values?.length) {
    throw new Error(text.removeHasValues(label));
  }
  if (value.op !== "-" && !values?.length) {
    throw new Error(text.setNeedsValues(label));
  }
  if (hasPreset && value.op !== "-") {
    throw new Error(text.presetOnlyRemove(label));
  }
  const pattern = ((hasName ? value.name : hasGlob ? value.glob : value.preset) as string).trim();
  if (hasName && pattern.toLowerCase() === "host" &&
      (value.op === "+" || (values?.length ?? 0) > 1)) {
    throw new Error(text.hostValues(label));
  }
  return {
    op: value.op as HeaderOp["op"],
    ...(hasName ? { name: pattern } : hasGlob ? { glob: pattern } : { preset: "proxy-disclosure" as const }),
    ...(values?.length ? { values } : {}),
  };
}

function isValidHeaderName(value: string) {
  return /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/.test(value);
}

function assertValidGlob(value: string, label: string, text: AppText["authConsole"]) {
  for (let index = 0; index < value.length; index += 1) {
    const character = value[index];
    if (character === "\\") {
      index += 1;
      if (index >= value.length) throw new Error(text.invalidGlob(label));
      continue;
    }
    if (character !== "[") continue;

    index += 1;
    if (value[index] === "^") index += 1;
    let ranges = 0;
    while (index < value.length && value[index] !== "]") {
      const [low, nextIndex] = readGlobClassCharacter(value, index, label, text);
      index = nextIndex;
      if (value[index] === "-") {
        const [high, rangeEnd] = readGlobClassCharacter(value, index + 1, label, text);
        if (high < low) throw new Error(text.invalidGlob(label));
        index = rangeEnd;
      }
      ranges += 1;
    }
    if (ranges === 0 || value[index] !== "]") {
      throw new Error(text.invalidGlob(label));
    }
  }
}

function readGlobClassCharacter(value: string, index: number, label: string, text: AppText["authConsole"]): [number, number] {
  if (index >= value.length || value[index] === "-" || value[index] === "]") {
    throw new Error(text.invalidGlob(label));
  }
  if (value[index] === "\\") {
    index += 1;
    if (index >= value.length) throw new Error(text.invalidGlob(label));
  }
  const codePoint = value.codePointAt(index);
  if (codePoint === undefined || codePoint === "/".codePointAt(0)) {
    throw new Error(text.invalidGlob(label));
  }
  return [codePoint, index + (codePoint > 0xffff ? 2 : 1)];
}

function decodeDirectiveToken(value: string, text: AppText["authConsole"]): DecodedDirectiveToken {
  const directiveToken = value.trim();
  const parts = directiveToken.split(".");
  if (parts.length !== 4 || parts[0] !== tokenFamily || parts[1] !== tokenVersion ||
      !/^[A-Za-z0-9_-]+$/.test(parts[3])) {
    throw new Error(text.tokenPrefix);
  }
  try {
    const decoded = new TextDecoder("utf-8", { fatal: true }).decode(base64URLDecode(parts[3]));
    if (parts[2] === "i") {
      return { source: "inline", payload: parsePayloadJSON(decoded, text) };
    }
    if (parts[2] === "r") {
      const parsed: unknown = JSON.parse(decoded);
      const spec = validateRemoteSpec(parsed, text);
      return { source: spec.type, spec };
    }
    throw new Error(text.tokenPrefix);
  } catch (err) {
    throw new Error(errorMessage(err, text.tokenDecodeFailed));
  }
}

function encodeDirectiveToken(editor: EditorState, payload: DirectivePayload) {
  const kind = editor.source === "inline" ? "i" : "r";
  const value = editor.source === "inline" ? JSON.stringify(payload) : JSON.stringify(buildRemoteSpec(editor));
  return `${tokenFamily}.${tokenVersion}.${kind}.${base64URL(value)}`;
}

function buildRemoteSpec(editor: EditorState): RemoteSpec {
  const headers = Object.fromEntries(editor.resolverHeaders.flatMap((header) => {
    const name = header.name.trim();
    return name ? [[name, header.value]] : [];
  }));
  return {
    type: editor.source === "redis" ? "redis" : "http",
    url: (editor.source === "redis" ? editor.redisURL : editor.httpURL).trim(),
    ...(editor.remoteKey ? { key: editor.remoteKey } : {}),
    ...(editor.source === "http" && Object.keys(headers).length ? { headers } : {}),
  };
}

function validateRemoteSpec(value: unknown, text: AppText["authConsole"]): RemoteSpec {
  if (!isRecord(value)) throw new Error(text.mustBe("RemoteSpec", "JSON object"));
  assertKnownKeys(value, ["type", "url", "key", "headers"], "RemoteSpec", text);
  if (value.type !== "http" && value.type !== "redis") {
    throw new Error(text.onlyValues("RemoteSpec.type", "http, redis"));
  }
  if (typeof value.url !== "string") throw new Error(text.nonEmptyString("RemoteSpec.url"));
  let parsedURL: URL;
  try {
    parsedURL = new URL(value.url);
  } catch {
    throw new Error(text.invalidRemoteURL);
  }
  if ((value.type === "http" && !["http:", "https:"].includes(parsedURL.protocol)) ||
      (value.type === "http" && Boolean(parsedURL.username || parsedURL.password)) ||
      (value.type === "redis" && !["redis:", "rediss:"].includes(parsedURL.protocol))) {
    throw new Error(text.invalidRemoteURL);
  }
  const key = value.key ?? "";
  if (typeof key !== "string" || (key !== "" && !isRemoteKeyValid(key)) ||
      (value.type === "redis" && key === "")) {
    throw new Error(text.invalidRedisKey);
  }
  let headers: Record<string, string> | undefined;
  if (value.headers !== undefined) {
    if (value.type !== "http" || !isRecord(value.headers)) {
      throw new Error(text.mustBe("RemoteSpec.headers", "string map"));
    }
    headers = {};
    const forbidden = new Set([
      "host", "content-length", "content-type", "connection", "proxy-connection", "keep-alive",
      "transfer-encoding", "upgrade", "trailer", "te",
    ]);
    for (const [name, headerValue] of Object.entries(value.headers)) {
      if (!isValidHeaderName(name) || forbidden.has(name.toLowerCase()) ||
          typeof headerValue !== "string" || /[\r\n]/.test(headerValue)) {
        throw new Error(text.invalidResolverHeader);
      }
      headers[name] = headerValue;
    }
  }
  return { type: value.type, url: value.url.trim(), ...(key ? { key } : {}), ...(headers ? { headers } : {}) };
}

function remoteSpecToEditor(editor: EditorState, spec: RemoteSpec): EditorState {
  return {
    ...editor,
    source: spec.type,
    remoteKey: spec.key ?? "",
    ...(spec.type === "http" ? {
      httpURL: spec.url,
      resolverHeaders: Object.entries(spec.headers ?? {}).map(([name, value]) => newResolverHeader(name, value)),
    } : { redisURL: spec.url }),
  };
}

function isRemoteKeyValid(value: string) {
  const bytes = new TextEncoder().encode(value);
  return Boolean(value) && value === value.trim() && bytes.length <= 256 && ![...value].some((char) => {
    const codePoint = char.codePointAt(0) ?? 0;
    return codePoint === 0 || codePoint < 0x20 || codePoint === 0x7f;
  });
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

function assertKnownKeys(value: Record<string, unknown>, keys: string[], label: string, text: AppText["authConsole"]) {
  const unknown = Object.keys(value).find((key) => !keys.includes(key));
  if (unknown) throw new Error(text.unknownField(label, unknown));
}

function normalizeRequestPath(value: string, text: AppText["authConsole"]) {
  const path = value.trim();
  if (!path) throw new Error(text.pathRequired);
  if (!path.startsWith("/") || path.startsWith("//")) {
    throw new Error(text.pathSameOrigin);
  }
  if (path.startsWith("/api/") || path === "/api" || path === "/health") {
    throw new Error(text.pathControlPlane);
  }
  return path;
}

function parseRequestHeaders(value: string, text: AppText["authConsole"]): Record<string, string> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value || "{}");
  } catch {
    throw new Error(text.invalidJSON("Request Headers"));
  }
  if (!isRecord(parsed)) throw new Error(text.mustBe("Request Headers", "JSON object"));
  const headers: Record<string, string> = {};
  for (const [name, headerValue] of Object.entries(parsed)) {
    if (typeof headerValue !== "string") {
      throw new Error(text.headerValueString(name));
    }
    if (name.toLowerCase() === "authorization") continue;
    headers[name] = headerValue;
  }
  return headers;
}

function formatResponseHeaders(headers: Headers) {
  return [...headers.entries()]
    .map(([name, value]) => `${name}: ${value}`)
    .join("\n");
}

function statusColor(status: number) {
  if (status >= 500) return "red";
  if (status >= 400) return "orange";
  if (status >= 300) return "blue";
  if (status >= 200) return "green";
  return "default";
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

function newHeaderOp(
  op: HeaderOp["op"],
  selector: HeaderOp["selector"],
  pattern: string,
  values: string[],
): HeaderOp {
  headerOpID += 1;
  return { key: `header-op-${headerOpID}`, op, selector, pattern, values };
}

function newResolverHeader(name: string, value: string): ResolverHeader {
  resolverHeaderID += 1;
  return { key: `resolver-header-${resolverHeaderID}`, name, value };
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
