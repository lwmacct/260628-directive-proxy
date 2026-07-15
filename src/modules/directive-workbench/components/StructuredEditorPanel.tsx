import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { Button, Checkbox, Flex, Form, Input, Segmented, Select, Typography } from "antd";
import type { CheckboxChangeEvent } from "antd/es/checkbox";
import type { ChangeEvent } from "react";
import { HeaderOperationsTable } from "./HeaderOperationsTable";
import { newHeaderOp, newResolverHeader } from "../constants";
import type { EditorState, HeaderOp } from "../types";
import type { Text } from "../../../shared/i18n";

const { Text: Label } = Typography;

export function StructuredEditorPanel(props: { editor: EditorState; text: Text["authConsole"]; onUpdate: (patch: Partial<EditorState>) => void }) {
  const { editor, text, onUpdate } = props;
  const updateHeaderOp = (key: string, patch: Partial<HeaderOp>) => onUpdate({ headerOps: editor.headerOps.map((item) => item.key === key ? { ...item, ...patch } : item) });
  return <>
    <Form layout="vertical">
      <Form.Item label={text.directiveSource}><Segmented options={[{ label: "Inline", value: "inline" }, { label: "HTTP API", value: "http" }, { label: "Redis", value: "redis" }]} value={editor.source} onChange={(source: EditorState["source"]) => onUpdate({ source })} /></Form.Item>
      {editor.source === "inline" ? <>
        <Form.Item label="Target URL"><Input value={editor.targetURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ targetURL: event.target.value })} /></Form.Item>
        <Form.Item label="Proxy URL"><Input allowClear placeholder="socks5://user:pass@127.0.0.1:1080" value={editor.proxyURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ proxyURL: event.target.value })} /></Form.Item>
        <Form.Item label="Join Path"><Checkbox checked={editor.joinPath} onChange={(event: CheckboxChangeEvent) => onUpdate({ joinPath: event.target.checked })}>{text.enabled}</Checkbox></Form.Item>
        <div className="header-section">
          <Form.Item label="Header Mode"><Select options={[{ label: "Patch", value: "patch" }, { label: "Replace", value: "replace" }]} value={editor.headerMode} onChange={(headerMode: EditorState["headerMode"]) => onUpdate({ headerMode })} /></Form.Item>
          <Flex align="center" justify="space-between" style={{ marginBottom: 12 }}><Label strong>{text.headerOps}</Label><Button icon={<PlusOutlined />} onClick={() => onUpdate({ headerOps: [...editor.headerOps, newHeaderOp("=", "name", "", [])] })}>{text.add}</Button></Flex>
          <HeaderOperationsTable items={editor.headerOps} text={text} onChange={updateHeaderOp} onRemove={(key) => onUpdate({ headerOps: editor.headerOps.filter((item) => item.key !== key) })} />
        </div>
      </> : <>
        <Form.Item label={editor.source === "http" ? text.httpResolverURL : text.redisURL}><Input placeholder={editor.source === "http" ? "https://policy.example.com/v1/resolve" : "redis://user:password@redis.example.com:6379/1"} value={editor.source === "http" ? editor.httpURL : editor.redisURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate(editor.source === "http" ? { httpURL: event.target.value } : { redisURL: event.target.value })} /></Form.Item>
        <Form.Item label={editor.source === "http" ? text.optionalRemoteKey : text.redisKey}><Input placeholder="team-a/service-a" value={editor.remoteKey} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ remoteKey: event.target.value })} /></Form.Item>
        {editor.source === "http" ? <><Form.Item label={text.resolverRequestHeaders}><Select mode="tags" open={false} placeholder="Content-Type, X-Tenant-*" value={editor.resolverRequestHeaders} onChange={(resolverRequestHeaders: string[]) => onUpdate({ resolverRequestHeaders })} /></Form.Item><Form.Item label={text.resolverHeaders}><Flex gap="small" vertical>{editor.resolverHeaders.map((header) => <Flex gap="small" key={header.key} wrap><Input placeholder="Authorization" style={{ flex: "1 1 160px", minWidth: 0 }} value={header.name} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ resolverHeaders: editor.resolverHeaders.map((item) => item.key === header.key ? { ...item, name: event.target.value } : item) })} /><Input placeholder="Bearer policy-token" style={{ flex: "1 1 160px", minWidth: 0 }} value={header.value} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ resolverHeaders: editor.resolverHeaders.map((item) => item.key === header.key ? { ...item, value: event.target.value } : item) })} /><Button aria-label={text.removeResolverHeader} icon={<DeleteOutlined />} onClick={() => onUpdate({ resolverHeaders: editor.resolverHeaders.filter((item) => item.key !== header.key) })} /></Flex>)}<Button icon={<PlusOutlined />} onClick={() => onUpdate({ resolverHeaders: [...editor.resolverHeaders, newResolverHeader("", "")] })}>{text.addResolverHeader}</Button></Flex></Form.Item></> : null}
      </>}
    </Form>
  </>;
}
