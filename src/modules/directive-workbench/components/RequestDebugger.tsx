import { SendOutlined } from "@ant-design/icons";
import { Alert, Button, Flex, Form, Input, Select, Space, Tag, Typography } from "antd";
import { useRef, useState, type ChangeEvent } from "react";
import type { Text } from "../../../shared/i18n";
import type { RequestResult } from "../types";
import { errorMessage, formatResponseHeaders, normalizeRequestPath, parseRequestHeaders, statusColor } from "../utils";

const { Text: Label } = Typography;

export function RequestDebugger(props: { text: Text["directiveConsole"]; directiveToken: string }) {
  const [method, setMethod] = useState("POST");
  const [path, setPath] = useState("/v1/resources");
  const [headers, setHeaders] = useState('{\n  "Content-Type": "application/json"\n}');
  const [body, setBody] = useState('{\n  "message": "Hello",\n  "metadata": {\n    "source": "workbench"\n  }\n}');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<RequestResult | null>(null);
  const controller = useRef<AbortController | null>(null);
  const bodyDisabled = method === "GET" || method === "HEAD";

  async function send() {
    try {
      if (!props.directiveToken) throw new Error(props.text.directiveNotReady);
      const requestController = new AbortController();
      controller.current?.abort();
      controller.current = requestController;
      setLoading(true);
      setError(null);
      setResult(null);
      const startedAt = performance.now();
      const response = await fetch(normalizeRequestPath(path, props.text), {
        method,
        credentials: "omit",
        headers: { ...parseRequestHeaders(headers, props.text), Authorization: `Bearer ${props.directiveToken}` },
        body: bodyDisabled ? undefined : body,
        signal: requestController.signal,
      });
      setResult({
        body: await response.text(),
        duration: Math.round(performance.now() - startedAt),
        headers: formatResponseHeaders(response.headers),
        status: response.status,
        statusText: response.statusText,
      });
    } catch (requestError) {
      setError(requestError instanceof DOMException && requestError.name === "AbortError"
        ? props.text.requestCancelled
        : errorMessage(requestError, props.text.requestFailed));
    } finally {
      controller.current = null;
      setLoading(false);
    }
  }

  return <Flex gap="middle" vertical>
    <Flex align="center" gap="small" justify="space-between" wrap>
      <Label type="secondary">{props.text.requestDescription}</Label>
      <Space>
        {loading ? <Button onClick={() => controller.current?.abort()}>{props.text.cancel}</Button> : null}
        <Button disabled={!props.directiveToken} icon={<SendOutlined />} loading={loading} onClick={() => void send()} type="primary">{props.text.send}</Button>
      </Space>
    </Flex>
    {!props.directiveToken ? <Alert showIcon title={props.text.directiveNotReady} type="warning" /> : null}
    <Flex gap="small" wrap>
      <Select className="request-method" options={["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"].map((value) => ({ label: value, value }))} value={method} onChange={setMethod} />
      <Input className="request-path" placeholder="/v1/resources" value={path} onChange={(event: ChangeEvent<HTMLInputElement>) => setPath(event.target.value)} />
    </Flex>
    <Form layout="vertical">
      <Form.Item label={props.text.requestHeaders}>
        <Input.TextArea autoSize={{ minRows: 4, maxRows: 10 }} className="request-code-input" value={headers} onChange={(event: ChangeEvent<HTMLTextAreaElement>) => setHeaders(event.target.value)} />
      </Form.Item>
      <Form.Item help={bodyDisabled ? props.text.bodyDisabled(method) : undefined} label={props.text.requestBody}>
        <Input.TextArea autoSize={{ minRows: 9, maxRows: 20 }} className="request-code-input" disabled={bodyDisabled} value={body} onChange={(event: ChangeEvent<HTMLTextAreaElement>) => setBody(event.target.value)} />
      </Form.Item>
    </Form>
    {error ? <Alert showIcon title={error} type="error" /> : null}
    {!error && !result ? <Alert showIcon title={props.text.waiting} type="info" /> : null}
    {result ? <Space orientation="vertical" size={12} style={{ width: "100%" }}>
      <Flex align="center" gap="small"><Tag color={statusColor(result.status)}>{result.status} {result.statusText}</Tag><Label type="secondary">{result.duration} ms</Label></Flex>
      <Form layout="vertical">
        <Form.Item label={props.text.responseHeaders}><Input.TextArea autoSize={{ minRows: 4, maxRows: 10 }} className="request-code-input" readOnly value={result.headers} /></Form.Item>
        <Form.Item label={props.text.responseBody}><Input.TextArea autoSize={{ minRows: 9, maxRows: 20 }} className="request-code-input" readOnly value={result.body} /></Form.Item>
      </Form>
    </Space> : null}
  </Flex>;
}
