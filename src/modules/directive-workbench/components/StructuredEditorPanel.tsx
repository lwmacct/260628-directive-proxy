import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { Button, Checkbox, Flex, Form, Input, Segmented, Select, Space, Tabs, Typography } from "antd";
import type { CheckboxChangeEvent } from "antd/es/checkbox";
import { useEffect, useState, type ChangeEvent } from "react";
import { HeaderOperationsTable } from "./HeaderOperationsTable";
import { newHeaderOp, newResolverHeader } from "../constants";
import type { EditorState, HeaderOp, ModuleSpec, RecoverySpec } from "../types";
import type { Text } from "../../../shared/i18n";

const { Text: Label } = Typography;

function ProgramEditor(props: { label: string; value: ModuleSpec[]; onChange: (value: ModuleSpec[]) => void }) {
  const { label, value, onChange } = props;
  const [raw, setRaw] = useState(() => JSON.stringify(value, null, 2));
  const [invalid, setInvalid] = useState(false);
  useEffect(() => { setRaw(JSON.stringify(value, null, 2)); }, [value]);
  const apply = () => {
    try {
      const parsed: unknown = JSON.parse(raw || "[]");
      if (!Array.isArray(parsed)) throw new Error("module program must be an array");
      onChange(parsed as ModuleSpec[]);
      setInvalid(false);
    } catch {
      setInvalid(true);
    }
  };
  return <Form.Item help={invalid ? "Invalid module program JSON" : undefined} label={label} validateStatus={invalid ? "error" : undefined}>
    <Input.TextArea autoSize={{ minRows: 4, maxRows: 12 }} onBlur={apply} onChange={(event) => setRaw(event.target.value)} value={raw} />
  </Form.Item>;
}

function RecoveryEditor(props: { value?: RecoverySpec; onChange: (value?: RecoverySpec) => void }) {
  const { value, onChange } = props;
  const [raw, setRaw] = useState(() => value ? JSON.stringify(value, null, 2) : "");
  const [invalid, setInvalid] = useState(false);
  useEffect(() => { setRaw(value ? JSON.stringify(value, null, 2) : ""); }, [value]);
  const apply = () => {
    try {
      const parsed = raw.trim() ? JSON.parse(raw) as RecoverySpec : undefined;
      onChange(parsed);
      setInvalid(false);
    } catch {
      setInvalid(true);
    }
  };
  return <Form.Item help={invalid ? "Invalid Recovery JSON" : undefined} label="Recovery Controller" validateStatus={invalid ? "error" : undefined}>
    <Input.TextArea autoSize={{ minRows: 6, maxRows: 18 }} placeholder='{ "controller": { "url": "https://control.example/recovery" }, "triggers": { "unexpected_status": { "expected": [{ "from": 200, "to": 299 }] } }, "budget": { "max_attempts": 3 } }' onBlur={apply} onChange={(event) => setRaw(event.target.value)} value={raw} />
  </Form.Item>;
}

export function StructuredEditorPanel(props: { editor: EditorState; text: Text["authConsole"]; onUpdate: (patch: Partial<EditorState>) => void }) {
  const { editor, text, onUpdate } = props;
  const updateHeaderOps = (field: "requestHeaderOps" | "responseHeaderOps", items: HeaderOp[]) => {
    onUpdate(field === "requestHeaderOps" ? { requestHeaderOps: items } : { responseHeaderOps: items });
  };
  const updateHeaderOp = (field: "requestHeaderOps" | "responseHeaderOps", key: string, patch: Partial<HeaderOp>) => {
    updateHeaderOps(field, editor[field].map((item) => item.key === key ? { ...item, ...patch } : item));
  };
  const headerOpsEditor = (field: "requestHeaderOps" | "responseHeaderOps") => <>
    <Flex align="center" gap="small" justify="space-between" style={{ marginBottom: 12 }} wrap>
      <Label strong>{text.headerOps}</Label>
      <Space>
        {field === "requestHeaderOps" ? <Select aria-label={text.headerMode} options={[{ label: "Patch", value: "patch" }, { label: "Replace", value: "replace" }]} style={{ width: 120 }} value={editor.requestHeaderMode} onChange={(requestHeaderMode: EditorState["requestHeaderMode"]) => onUpdate({ requestHeaderMode })} /> : null}
        <Button icon={<PlusOutlined />} onClick={() => updateHeaderOps(field, [...editor[field], newHeaderOp("=", "name", "", [])])}>{text.add}</Button>
      </Space>
    </Flex>
    <HeaderOperationsTable items={editor[field]} text={text} onChange={(key, patch) => updateHeaderOp(field, key, patch)} onRemove={(key) => updateHeaderOps(field, editor[field].filter((item) => item.key !== key))} />
  </>;
  return <>
    <Form layout="vertical">
      <Form.Item label={text.directiveSource}><Segmented options={[{ label: "Inline", value: "inline" }, { label: "HTTP API", value: "http" }, { label: "Redis", value: "redis" }]} value={editor.source} onChange={(source: EditorState["source"]) => onUpdate({ source })} /></Form.Item>
      {editor.source === "inline" ? <>
        <Form.Item label="Target URL"><Input value={editor.targetURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ targetURL: event.target.value })} /></Form.Item>
        <Form.Item label="Proxy URL"><Input allowClear placeholder="socks5://user:pass@127.0.0.1:1080" value={editor.proxyURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ proxyURL: event.target.value })} /></Form.Item>
        <Form.Item label="Join Path"><Checkbox checked={editor.joinPath} onChange={(event: CheckboxChangeEvent) => onUpdate({ joinPath: event.target.checked })}>{text.enabled}</Checkbox></Form.Item>
        <div className="header-section">
          <Tabs items={[
            {
              key: "request",
              label: text.requestHeaderPolicy,
              children: <>
                <Form.Item><Checkbox checked={editor.preserveProxyDisclosure} onChange={(event: CheckboxChangeEvent) => onUpdate({ preserveProxyDisclosure: event.target.checked })}>{text.preserveProxyDisclosure}</Checkbox></Form.Item>
                {headerOpsEditor("requestHeaderOps")}
              </>,
            },
            { key: "response", label: text.responseHeaderPolicy, children: headerOpsEditor("responseHeaderOps") },
          ]} />
        </div>
        <ProgramEditor label="Request modules" value={editor.requestProgram} onChange={(requestProgram) => onUpdate({ requestProgram })} />
        <ProgramEditor label="Attempt modules" value={editor.attemptProgram} onChange={(attemptProgram) => onUpdate({ attemptProgram })} />
      </> : <>
        <Form.Item label={editor.source === "http" ? text.httpResolverURL : text.redisURL}><Input placeholder={editor.source === "http" ? "https://policy.example.com/v1/resolve" : "redis://user:password@redis.example.com:6379/1"} value={editor.source === "http" ? editor.httpURL : editor.redisURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate(editor.source === "http" ? { httpURL: event.target.value } : { redisURL: event.target.value })} /></Form.Item>
        <Form.Item label={editor.source === "http" ? text.optionalRemoteKey : text.redisKey}><Input placeholder="team-a/service-a" value={editor.remoteKey} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ remoteKey: event.target.value })} /></Form.Item>
        {editor.source === "http" ? <><Form.Item label={text.resolverRequestHeaders}><Select mode="tags" open={false} placeholder="Content-Type, X-Tenant-*" value={editor.resolverRequestHeaders} onChange={(resolverRequestHeaders: string[]) => onUpdate({ resolverRequestHeaders })} /></Form.Item><Form.Item label={text.resolverHeaders}><Flex gap="small" vertical>{editor.resolverHeaders.map((header) => <Flex gap="small" key={header.key} wrap><Input placeholder="Authorization" style={{ flex: "1 1 160px", minWidth: 0 }} value={header.name} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ resolverHeaders: editor.resolverHeaders.map((item) => item.key === header.key ? { ...item, name: event.target.value } : item) })} /><Input placeholder="Bearer policy-token" style={{ flex: "1 1 160px", minWidth: 0 }} value={header.value} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ resolverHeaders: editor.resolverHeaders.map((item) => item.key === header.key ? { ...item, value: event.target.value } : item) })} /><Button aria-label={text.removeResolverHeader} icon={<DeleteOutlined />} onClick={() => onUpdate({ resolverHeaders: editor.resolverHeaders.filter((item) => item.key !== header.key) })} /></Flex>)}<Button icon={<PlusOutlined />} onClick={() => onUpdate({ resolverHeaders: [...editor.resolverHeaders, newResolverHeader("", "")] })}>{text.addResolverHeader}</Button></Flex></Form.Item></> : null}
        <ProgramEditor label="Request modules" value={editor.requestProgram} onChange={(requestProgram) => onUpdate({ requestProgram })} />
      </>}
      <RecoveryEditor value={editor.recovery} onChange={(recovery) => onUpdate({ recovery })} />
    </Form>
  </>;
}
