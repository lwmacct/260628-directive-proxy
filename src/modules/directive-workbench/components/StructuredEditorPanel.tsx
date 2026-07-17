import { PlusOutlined, QuestionCircleOutlined } from "@ant-design/icons";
import { Alert, Button, Checkbox, Flex, Form, Input, Segmented, Select, Space, Tabs, Tooltip, Typography } from "antd";
import type { CheckboxChangeEvent } from "antd/es/checkbox";
import type { ChangeEvent } from "react";
import type { Text } from "../../../shared/i18n";
import { newHeaderMutation } from "../constants";
import type { EditorState, HeaderMutation } from "../types";
import { HeaderMutationsTable } from "./HeaderMutationsTable";
import { ModuleProgramEditor } from "./ModuleProgramEditor";
import { RecoveryEditor } from "./RecoveryEditor";

const { Text: Label } = Typography;

export function StructuredEditorPanel(props: {
  editor: EditorState;
  text: Text["authConsole"];
  onUpdate: (patch: Partial<EditorState>) => void;
}) {
  const { editor, text, onUpdate } = props;
  type HeaderField = "headerMutations" | "resolverHeaderMutations";
  const updateHeaderMutations = (field: HeaderField, items: HeaderMutation[]) => {
    if (field === "headerMutations") onUpdate({ headerMutations: items });
    else onUpdate({ resolverHeaderMutations: items });
  };
  const updateHeaderMutation = (field: HeaderField, key: string, patch: Partial<HeaderMutation>) => {
    updateHeaderMutations(field, editor[field].map((item) => item.key === key ? { ...item, ...patch } : item));
  };
  const headerMutationsEditor = (field: HeaderField, showSide: boolean) => <Flex gap="small" vertical>
    <Flex align="center" gap="small" justify="space-between" wrap>
      <Label strong>{text.headerMutations}</Label>
      <Space wrap>
        <Label type="secondary">{text.headerMode}</Label>
        <Select
          aria-label={text.headerMode}
          options={[{ label: "Patch", value: "patch" }, { label: "Replace", value: "replace" }]}
          style={{ width: 120 }}
          value={field === "headerMutations" ? editor.requestHeaderMode : editor.resolverHeaderMode}
          onChange={(mode: EditorState["requestHeaderMode"]) => field === "headerMutations" ? onUpdate({ requestHeaderMode: mode }) : onUpdate({ resolverHeaderMode: mode })}
        />
        <Button icon={<PlusOutlined />} onClick={() => updateHeaderMutations(field, [...editor[field], newHeaderMutation("set", "name", "", [])])}>{text.add}</Button>
      </Space>
    </Flex>
    <HeaderMutationsTable items={editor[field]} showSide={showSide} text={text} onChange={(key, patch) => updateHeaderMutation(field, key, patch)} onRemove={(key) => updateHeaderMutations(field, editor[field].filter((item) => item.key !== key))} />
  </Flex>;

  const remoteBasics = editor.source === "file" ? <Form.Item label={text.filePath}>
    <Input placeholder="team-a/services/primary.json" value={editor.filePath} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ filePath: event.target.value })} />
  </Form.Item> : <>
    <Form.Item label={editor.source === "http" ? text.httpResolverURL : text.redisURL}>
      <Input
        placeholder={editor.source === "http" ? "https://policy.example.com/v1/resolve" : "redis://user:password@redis.example.com:6379/1"}
        value={editor.source === "http" ? editor.httpURL : editor.redisURL}
        onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate(editor.source === "http" ? { httpURL: event.target.value } : { redisURL: event.target.value })}
      />
    </Form.Item>
    {editor.source === "redis" ? <Form.Item label={text.redisKey}><Input placeholder="team-a/service-a" value={editor.remoteKey} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ remoteKey: event.target.value })} /></Form.Item> : null}
    {editor.source === "http" ? <>
      <Form.Item><Checkbox checked={editor.resolverPreserveProxyDisclosure} onChange={(event: CheckboxChangeEvent) => onUpdate({ resolverPreserveProxyDisclosure: event.target.checked })}>{text.preserveProxyDisclosure}</Checkbox></Form.Item>
      <Form.Item label={<Space size={6}>{text.resolverHeaders}<Tooltip
        styles={{ container: { maxWidth: 520 } }}
        title={<><Label strong>{text.resolverHeaderNoticeTitle}</Label><ul className="policy-notice-list">
          <li>{text.resolverHeaderNoticeBaseline}</li>
          <li>{text.resolverHeaderNoticeBeforeMutations}</li>
          <li>{text.resolverHeaderNoticeAfterMutations}</li>
          <li>{text.resolverHeaderNoticeOverride}</li>
        </ul></>}
      ><QuestionCircleOutlined aria-label={text.resolverHeaderNoticeTitle} className="help-icon" tabIndex={0} /></Tooltip></Space>}>
        {headerMutationsEditor("resolverHeaderMutations", false)}
      </Form.Item>
    </> : null}
  </>;

  const basics = editor.source === "inline" ? <>
    <Form.Item label={text.targetURL}><Input placeholder="https://api.example.com/v1" value={editor.targetURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ targetURL: event.target.value })} /></Form.Item>
    <Form.Item label={text.proxyURL}><Input allowClear placeholder="socks5://user:pass@127.0.0.1:1080" value={editor.proxyURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ proxyURL: event.target.value })} /></Form.Item>
    <Form.Item label={text.joinPath}><Checkbox checked={editor.joinPath} onChange={(event: CheckboxChangeEvent) => onUpdate({ joinPath: event.target.checked })}>{text.enabled}</Checkbox></Form.Item>
  </> : <>
    {remoteBasics}
    <Alert showIcon title={text.remoteSpecOnlyHint} type="info" />
  </>;

  const items = [
    { key: "basics", label: text.basics, children: basics },
    ...(editor.source === "inline" ? [{
      key: "headers",
      label: text.headers,
      children: <Flex gap="middle" vertical>
        <Checkbox checked={editor.preserveProxyDisclosure} onChange={(event: CheckboxChangeEvent) => onUpdate({ preserveProxyDisclosure: event.target.checked })}>{text.preserveProxyDisclosure}</Checkbox>
        {headerMutationsEditor("headerMutations", true)}
      </Flex>,
    }, {
      key: "modules",
      label: text.modules,
      children: <Flex gap="large" vertical>
        <div><Label strong>{text.requestModules}</Label><div className="section-description">{text.requestModulesDescription}</div><ModuleProgramEditor text={text} value={editor.requestProgram} onChange={(requestProgram) => onUpdate({ requestProgram })} /></div>
        <div><Label strong>{text.attemptModules}</Label><div className="section-description">{text.attemptModulesDescription}</div><ModuleProgramEditor text={text} value={editor.attemptProgram} onChange={(attemptProgram) => onUpdate({ attemptProgram })} /></div>
      </Flex>,
    },
    {
      key: "recovery",
      label: text.recovery,
      children: <RecoveryEditor text={text} value={editor.recovery} onChange={(recovery) => onUpdate({ recovery })} />,
    }] : []),
  ];

  return <Form layout="vertical">
    <Form.Item label={text.directiveSource}>
      <Segmented
        block
        options={[
          { label: text.inlineSource, value: "inline" },
          { label: text.httpSource, value: "http" },
          { label: text.redisSource, value: "redis" },
          { label: text.fileSource, value: "file" },
        ]}
        value={editor.source}
        onChange={(source: EditorState["source"]) => onUpdate({ source })}
      />
    </Form.Item>
    <Tabs className="builder-tabs" items={items} />
  </Form>;
}
