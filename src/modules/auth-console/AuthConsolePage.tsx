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
import { useEffect, useMemo, useRef, useState, type ChangeEvent } from "react";
import { apiFetch } from "../../app/auth";
import { useText, type Text as AppText } from "../../shared/i18n";

const { Text } = Typography;
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
  resolverRequestHeaders: string[];
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

type RemoteSpec = {
  type: "http" | "redis";
  url: string;
  key?: string;
  headers?: Record<string, string>;
  request_headers?: string[];
};

type DirectiveDocument =
  | { kind: "inline"; payload: DirectivePayload }
  | { kind: "remote"; remote: RemoteSpec };

type DirectiveCodecResponse = {
  token: string;
  document: DirectiveDocument;
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
  remoteKey: "team-a/service-a",
  httpURL: "https://policy.example.com/v1/resolve",
  redisURL: "redis://user:password@redis.example.com:6379/1",
  resolverHeaders: [newResolverHeader("Authorization", "Bearer policy-token")],
  resolverRequestHeaders: ["Content-Type", "X-Tenant"],
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
  const [tokenInput, setTokenInput] = useState("");
  const [directiveToken, setDirectiveToken] = useState("");
  const [activeSource, setActiveSource] = useState<"payload" | "token">("payload");
  const [error, setError] = useState<string | null>(null);
  const [requestMethod, setRequestMethod] = useState("POST");
  const [requestPath, setRequestPath] = useState("/v1/resources");
  const [requestHeaders, setRequestHeaders] = useState(
    '{\n  "Content-Type": "application/json"\n}',
  );
  const [requestBody, setRequestBody] = useState(
    '{\n  "message": "Hello",\n  "metadata": {\n    "source": "workbench"\n  }\n}',
  );
  const [requestLoading, setRequestLoading] = useState(false);
  const [requestError, setRequestError] = useState<string | null>(null);
  const [requestResult, setRequestResult] = useState<RequestResult | null>(null);
  const requestController = useRef<AbortController | null>(null);

  const payload = useMemo(() => buildPayload(editor), [editor]);
  const sourceDirty = activeSource === "payload"
    ? payloadInput !== formatPayload(payload)
    : tokenInput !== directiveToken;

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void encodeDirectiveDocument(editor, payload, controller.signal)
        .then((result) => {
          setDirectiveToken(result.token);
          setTokenInput(result.token);
        })
        .catch((err: unknown) => {
          if (!(err instanceof DOMException && err.name === "AbortError")) {
            setDirectiveToken("");
          }
        });
    }, 200);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [editor, payload]);

  function updateEditor(patch: Partial<EditorState>) {
    const next = { ...editor, ...patch };
    setDirectiveToken("");
    setEditor(next);
    syncInputs(buildPayload(next));
  }

  function syncInputs(nextPayload: DirectivePayload) {
    setPayloadInput(formatPayload(nextPayload));
    setError(null);
  }

  function updateDirectiveSource(source: DirectiveSource) {
    setActiveSource(source === "inline" ? "payload" : "token");
    updateEditor({ source });
  }

  async function applyPayloadInput() {
    try {
      const parsed: unknown = JSON.parse(payloadInput);
      const result = await directiveCodecRequest("encode", { kind: "inline", payload: parsed as DirectivePayload });
      if (result.document.kind !== "inline") throw new Error(t.authConsole.payloadParseFailed);
      applyPayload(result.document.payload);
      setDirectiveToken(result.token);
      setTokenInput(result.token);
      void message.success(t.authConsole.payloadApplied);
    } catch (err) {
      setError(errorMessage(err, t.authConsole.payloadParseFailed));
    }
  }

  async function applyTokenInput() {
    try {
      const decoded = await directiveCodecRequest("decode", { token: tokenInput });
      if (decoded.document.kind === "inline") {
        applyPayload(decoded.document.payload);
      } else {
        const next = remoteSpecToEditor(editor, decoded.document.remote);
        setEditor(next);
        setTokenInput(decoded.token);
        setDirectiveToken(decoded.token);
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
    syncInputs(nextPayload);
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
      if (!directiveToken) throw new Error(t.authConsole.directiveNotReady);
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
                        : "redis://user:password@redis.example.com:6379/1"}
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
                      placeholder="team-a/service-a"
                      status={editor.source === "redis" && !isRemoteKeyValid(editor.remoteKey) ? "error" : undefined}
                      value={editor.remoteKey}
                      onChange={(event: ChangeEvent<HTMLInputElement>) =>
                        updateEditor({ remoteKey: event.target.value })
                      }
                    />
                  </Form.Item>
                  {editor.source === "http" ? (
                    <>
                      <Form.Item label={t.authConsole.resolverRequestHeaders}>
                        <Select
                          mode="tags"
                          open={false}
                          placeholder="Content-Type, X-Tenant-*"
                          value={editor.resolverRequestHeaders}
                          onChange={(resolverRequestHeaders: string[]) => updateEditor({ resolverRequestHeaders })}
                        />
                      </Form.Item>
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
                    </>
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
                      placeholder="dproxy.14.i..."
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
                      placeholder="dproxy.14.r..."
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
          placeholder="/v1/resources"
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
    ...(editor.source === "http" && editor.resolverRequestHeaders.length
      ? { request_headers: editor.resolverRequestHeaders }
      : {}),
  };
}

function remoteSpecToEditor(editor: EditorState, spec: RemoteSpec): EditorState {
  return {
    ...editor,
    source: spec.type,
    remoteKey: spec.key ?? "",
    ...(spec.type === "http" ? {
      httpURL: spec.url,
      resolverHeaders: Object.entries(spec.headers ?? {}).map(([name, value]) => newResolverHeader(name, value)),
      resolverRequestHeaders: spec.request_headers ?? [],
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function encodeDirectiveDocument(editor: EditorState, payload: DirectivePayload, signal?: AbortSignal) {
  const document: DirectiveDocument = editor.source === "inline"
    ? { kind: "inline", payload }
    : { kind: "remote", remote: buildRemoteSpec(editor) };
  return directiveCodecRequest("encode", document, signal);
}

async function directiveCodecRequest(
  action: "encode" | "decode",
  body: DirectiveDocument | { token: string },
  signal?: AbortSignal,
): Promise<DirectiveCodecResponse> {
  const response = await apiFetch(`/api/admin/directives/${action}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
  });
  if (!response.ok) {
    let message = `Directive ${action} failed (${response.status})`;
    try {
      const errorBody = await response.json() as { detail?: string };
      if (errorBody.detail) message = errorBody.detail;
    } catch {
      // Keep the status-based message when the response is not JSON.
    }
    throw new Error(message);
  }
  return response.json() as Promise<DirectiveCodecResponse>;
}

function normalizeRequestPath(value: string, text: AppText["authConsole"]) {
  const path = value.trim();
  if (!path) throw new Error(text.pathRequired);
  if (!path.startsWith("/") || path.startsWith("//")) {
    throw new Error(text.pathSameOrigin);
  }
  if (path.startsWith("/api/") || path === "/api" || path === "/health") {
    throw new Error(text.pathReservedAPI);
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
