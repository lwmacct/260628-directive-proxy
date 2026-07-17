import { PlusOutlined } from "@ant-design/icons";
import { Alert, Button, Checkbox, Flex, Form, Input, Segmented, Select, Space, Tabs, Typography } from "antd";
import type { CheckboxChangeEvent } from "antd/es/checkbox";
import type { ChangeEvent } from "react";
import type { Text } from "../../../shared/i18n";
import { newHeaderOp } from "../constants";
import type { EditorState, HeaderOp } from "../types";
import { HeaderOperationsTable } from "./HeaderOperationsTable";
import { KeyValueEditor } from "./KeyValueEditor";
import { ModuleProgramEditor } from "./ModuleProgramEditor";
import { RecoveryEditor } from "./RecoveryEditor";

const { Text: Label } = Typography;

export function StructuredEditorPanel(props: {
  editor: EditorState;
  text: Text["authConsole"];
  onUpdate: (patch: Partial<EditorState>) => void;
}) {
  const { editor, text, onUpdate } = props;
  const updateHeaderOps = (field: "requestHeaderOps" | "responseHeaderOps", items: HeaderOp[]) => {
    onUpdate(field === "requestHeaderOps" ? { requestHeaderOps: items } : { responseHeaderOps: items });
  };
  const updateHeaderOp = (field: "requestHeaderOps" | "responseHeaderOps", key: string, patch: Partial<HeaderOp>) => {
    updateHeaderOps(field, editor[field].map((item) => item.key === key ? { ...item, ...patch } : item));
  };
  const headerOpsEditor = (field: "requestHeaderOps" | "responseHeaderOps") => <Flex gap="small" vertical>
    <Flex align="center" gap="small" justify="space-between" wrap>
      <Label strong>{text.headerOps}</Label>
      <Space wrap>
        {field === "requestHeaderOps" ? <Select aria-label={text.headerMode} options={[{ label: "Patch", value: "patch" }, { label: "Replace", value: "replace" }]} style={{ width: 120 }} value={editor.requestHeaderMode} onChange={(requestHeaderMode: EditorState["requestHeaderMode"]) => onUpdate({ requestHeaderMode })} /> : null}
        <Button icon={<PlusOutlined />} onClick={() => updateHeaderOps(field, [...editor[field], newHeaderOp("=", "name", "", [])])}>{text.add}</Button>
      </Space>
    </Flex>
    <HeaderOperationsTable items={editor[field]} text={text} onChange={(key, patch) => updateHeaderOp(field, key, patch)} onRemove={(key) => updateHeaderOps(field, editor[field].filter((item) => item.key !== key))} />
  </Flex>;

  const basics = editor.source === "inline" ? <>
    <Form.Item label={text.targetURL}><Input placeholder="https://api.example.com/v1" value={editor.targetURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ targetURL: event.target.value })} /></Form.Item>
    <Form.Item label={text.proxyURL}><Input allowClear placeholder="socks5://user:pass@127.0.0.1:1080" value={editor.proxyURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ proxyURL: event.target.value })} /></Form.Item>
    <Form.Item label={text.joinPath}><Checkbox checked={editor.joinPath} onChange={(event: CheckboxChangeEvent) => onUpdate({ joinPath: event.target.checked })}>{text.enabled}</Checkbox></Form.Item>
  </> : <>
    <Form.Item label={editor.source === "http" ? text.httpResolverURL : text.redisURL}>
      <Input
        placeholder={editor.source === "http" ? "https://policy.example.com/v1/resolve" : "redis://user:password@redis.example.com:6379/1"}
        value={editor.source === "http" ? editor.httpURL : editor.redisURL}
        onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate(editor.source === "http" ? { httpURL: event.target.value } : { redisURL: event.target.value })}
      />
    </Form.Item>
    <Form.Item label={editor.source === "http" ? text.optionalRemoteKey : text.redisKey}><Input placeholder="team-a/service-a" value={editor.remoteKey} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ remoteKey: event.target.value })} /></Form.Item>
    {editor.source === "http" ? <>
      <Form.Item label={text.resolverRequestHeaders}><Select mode="tags" open={false} placeholder="Content-Type, X-Tenant-*" style={{ width: "100%" }} value={editor.resolverRequestHeaders} onChange={(resolverRequestHeaders: string[]) => onUpdate({ resolverRequestHeaders })} /></Form.Item>
      <Form.Item label={text.resolverHeaders}>
        <KeyValueEditor addLabel={text.addResolverHeader} items={editor.resolverHeaders} removeLabel={text.removeResolverHeader} onChange={(resolverHeaders) => onUpdate({ resolverHeaders })} />
      </Form.Item>
    </> : null}
  </>;

  const items = [
    { key: "basics", label: text.basics, children: basics },
    ...(editor.source === "inline" ? [{
      key: "headers",
      label: text.headers,
      children: <Tabs items={[
        {
          key: "request",
          label: text.requestHeaderPolicy,
          children: <Flex gap="middle" vertical>
            <Checkbox checked={editor.preserveProxyDisclosure} onChange={(event: CheckboxChangeEvent) => onUpdate({ preserveProxyDisclosure: event.target.checked })}>{text.preserveProxyDisclosure}</Checkbox>
            {headerOpsEditor("requestHeaderOps")}
          </Flex>,
        },
        { key: "response", label: text.responseHeaderPolicy, children: headerOpsEditor("responseHeaderOps") },
      ]} />,
    }] : []),
    {
      key: "modules",
      label: text.modules,
      children: <Flex gap="large" vertical>
        <div><Label strong>{text.requestModules}</Label><div className="section-description">{text.requestModulesDescription}</div><ModuleProgramEditor text={text} value={editor.requestProgram} onChange={(requestProgram) => onUpdate({ requestProgram })} /></div>
        {editor.source === "inline" ? <div><Label strong>{text.attemptModules}</Label><div className="section-description">{text.attemptModulesDescription}</div><ModuleProgramEditor text={text} value={editor.attemptProgram} onChange={(attemptProgram) => onUpdate({ attemptProgram })} /></div> : <Alert showIcon title={text.remoteAttemptModulesHint} type="info" />}
      </Flex>,
    },
    {
      key: "recovery",
      label: text.recovery,
      children: <RecoveryEditor text={text} value={editor.recovery} onChange={(recovery) => onUpdate({ recovery })} />,
    },
  ];

  return <Form layout="vertical">
    <Form.Item label={text.directiveSource}>
      <Segmented
        block
        options={[
          { label: text.inlineSource, value: "inline" },
          { label: text.httpSource, value: "http" },
          { label: text.redisSource, value: "redis" },
        ]}
        value={editor.source}
        onChange={(source: EditorState["source"]) => onUpdate({ source })}
      />
    </Form.Item>
    <Tabs className="builder-tabs" items={items} />
  </Form>;
}
