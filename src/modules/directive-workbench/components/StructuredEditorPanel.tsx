import { PlusOutlined, QuestionCircleOutlined } from "@ant-design/icons";
import { Button, Checkbox, Flex, Form, Input, Select, Space, Tabs, Tooltip, Typography } from "antd";
import type { CheckboxChangeEvent } from "antd/es/checkbox";
import type { ChangeEvent } from "react";
import type { Text } from "../../../shared/i18n";
import { newHeaderMutation } from "../constants";
import type { DirectiveSource, EditorState, HeaderMutation } from "../types";
import { HeaderMutationsTable } from "./HeaderMutationsTable";
import { KeyValueEditor } from "./KeyValueEditor";
import { ModulesEditor } from "./ModulesEditor";
import { RecoveryEditor } from "./RecoveryEditor";

const { Text: Label } = Typography;

export function StructuredEditorPanel(props: {
  editor: EditorState;
  source: DirectiveSource;
  text: Text["authConsole"];
  onUpdate: (patch: Partial<EditorState>) => void;
}) {
  const { editor, source, text, onUpdate } = props;
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
        <Button icon={<PlusOutlined />} onClick={() => updateHeaderMutations(field, [...editor[field], newHeaderMutation("set", "name", "", [])])}>{text.add}</Button>
      </Space>
    </Flex>
    <HeaderMutationsTable items={editor[field]} showSide={showSide} text={text} onChange={(key, patch) => updateHeaderMutation(field, key, patch)} onRemove={(key) => updateHeaderMutations(field, editor[field].filter((item) => item.key !== key))} />
  </Flex>;

  const target = <>
    <Form.Item label={text.targetMode}><Select
      options={[{ label: text.baseURL, value: "base" }, { label: text.exactURL, value: "exact" }]}
      value={editor.targetMode}
      onChange={(targetMode: EditorState["targetMode"]) => onUpdate({ targetMode })}
    /></Form.Item>
    <Form.Item label={editor.targetMode === "base" ? text.baseURL : text.exactURL}><Input placeholder="https://api.example.com/v1" value={editor.targetURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ targetURL: event.target.value })} /></Form.Item>
    <Form.Item label={text.proxyURL}><Input allowClear placeholder="socks5://user:pass@127.0.0.1:1080" value={editor.proxyURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ proxyURL: event.target.value })} /></Form.Item>
  </>;

  if (source === "redis") {
    return <Form layout="vertical">
      <Form.Item label={text.redisURL}><Input placeholder="redis://user:password@redis.example.com:6379/1" value={editor.redisURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ redisURL: event.target.value })} /></Form.Item>
      <Form.Item label={text.redisKey}><Input placeholder="team-a/service-a" value={editor.remoteKey} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ remoteKey: event.target.value })} /></Form.Item>
    </Form>;
  }

  if (source === "file") {
    return <Form layout="vertical">
      <Form.Item label={text.filePath}><Input placeholder="team-a/services/primary.json" value={editor.filePath} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ filePath: event.target.value })} /></Form.Item>
    </Form>;
  }

  if (source === "http") {
    const httpItems = [{
      key: "endpoint",
      label: text.endpoint,
      children: <Form.Item label={text.httpResolverURL}><Input placeholder="https://policy.example.com/v1/resolve" value={editor.httpURL} onChange={(event: ChangeEvent<HTMLInputElement>) => onUpdate({ httpURL: event.target.value })} /></Form.Item>,
    }, {
      key: "headers",
      label: text.headers,
      children: <Flex gap="middle" vertical>
        <Checkbox checked={editor.resolverPreserveProxyDisclosure} onChange={(event: CheckboxChangeEvent) => onUpdate({ resolverPreserveProxyDisclosure: event.target.checked })}>{text.preserveProxyDisclosure}</Checkbox>
        <div>
          <Space size={6} style={{ marginBottom: 12 }}><Label strong>{text.resolverHeaders}</Label><Tooltip
            styles={{ container: { maxWidth: 520 } }}
            title={<><Label strong>{text.resolverHeaderNoticeTitle}</Label><ul className="policy-notice-list">
              <li>{text.resolverHeaderNoticeBaseline}</li>
              <li>{text.resolverHeaderNoticeBeforeMutations}</li>
              <li>{text.resolverHeaderNoticeAfterMutations}</li>
              <li>{text.resolverHeaderNoticeOverride}</li>
            </ul></>}
          ><QuestionCircleOutlined aria-label={text.resolverHeaderNoticeTitle} className="help-icon" tabIndex={0} /></Tooltip></Space>
          {headerMutationsEditor("resolverHeaderMutations", false)}
        </div>
      </Flex>,
    }];
    return <Form layout="vertical"><Tabs className="builder-tabs" items={httpItems} /></Form>;
  }

  const inlineItems = [
    { key: "target", label: text.target, children: target },
    {
      key: "metadata",
      label: text.metadata,
      children: <KeyValueEditor
        addLabel={text.addMetadataField}
        items={editor.metadataFields}
        maxItems={15}
        namePlaceholder={text.metadataKeyPlaceholder}
        removeLabel={text.removeMetadataField}
        valuePlaceholder={text.metadataValuePlaceholder}
        onChange={(metadataFields) => onUpdate({ metadataFields })}
      />,
    },
    {
      key: "headers",
      label: text.headers,
      children: <Flex gap="middle" vertical>
        <Checkbox checked={editor.preserveProxyDisclosure} onChange={(event: CheckboxChangeEvent) => onUpdate({ preserveProxyDisclosure: event.target.checked })}>{text.preserveProxyDisclosure}</Checkbox>
        {headerMutationsEditor("headerMutations", true)}
      </Flex>,
    }, {
      key: "modules",
      label: text.modules,
      children: <div><Label strong>{text.orderedModules}</Label><div className="section-description">{text.orderedModulesDescription}</div><ModulesEditor text={text} value={editor.modules} onChange={(modules) => onUpdate({ modules })} /></div>,
    },
    {
      key: "recovery",
      label: text.recovery,
      children: <RecoveryEditor text={text} value={editor.recovery} onChange={(recovery) => onUpdate({ recovery })} />,
    },
  ];

  return <Form layout="vertical"><Tabs className="builder-tabs" items={inlineItems} /></Form>;
}
