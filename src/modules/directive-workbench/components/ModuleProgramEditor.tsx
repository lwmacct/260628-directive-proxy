import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { Button, Flex, Input, Select, Table, Typography } from "antd";
import type { TableColumnsType } from "antd";
import type { ChangeEvent } from "react";
import type { Text } from "../../../shared/i18n";
import { newModuleSpec } from "../constants";
import type { EditorModuleSpec } from "../types";

function ModuleConfigInput(props: { item: EditorModuleSpec; onChange: (patch: Partial<EditorModuleSpec>) => void; invalidText: string }) {
  const change = (configText: string) => {
    try {
      props.onChange({ configText, config: JSON.parse(configText || "{}") as unknown, configValid: true });
    } catch {
      props.onChange({ configText, configValid: false });
    }
  };
  return <Flex gap={4} vertical>
    <Input.TextArea
      autoSize={{ minRows: 2, maxRows: 8 }}
      className="source-input"
      status={props.item.configValid ? undefined : "error"}
      value={props.item.configText}
      onChange={(event: ChangeEvent<HTMLTextAreaElement>) => change(event.target.value)}
    />
    {!props.item.configValid ? <Typography.Text type="danger">{props.invalidText}</Typography.Text> : null}
  </Flex>;
}

export function ModuleProgramEditor(props: {
  text: Text["authConsole"];
  value: EditorModuleSpec[];
  onChange: (value: EditorModuleSpec[]) => void;
}) {
  const update = (key: string, patch: Partial<EditorModuleSpec>) => {
    props.onChange(props.value.map((item) => item.key === key ? { ...item, ...patch } : item));
  };
  const columns: TableColumnsType<EditorModuleSpec> = [
    {
      title: props.text.moduleScope,
      dataIndex: "scope",
      width: 150,
      render: (_, item) => <Select
        options={[{ label: props.text.exchangeScope, value: "exchange" }, { label: props.text.attemptScope, value: "attempt" }]}
        value={item.scope}
        onChange={(scope: EditorModuleSpec["scope"]) => update(item.key, { scope })}
      />,
    },
    {
      title: props.text.moduleID,
      dataIndex: "id",
      width: 190,
      render: (_, item) => <Input placeholder="capture" value={item.id} onChange={(event: ChangeEvent<HTMLInputElement>) => update(item.key, { id: event.target.value })} />,
    },
    {
      title: props.text.moduleName,
      dataIndex: "module",
      width: 240,
      render: (_, item) => <Input placeholder="builtin.capture" value={item.module} onChange={(event: ChangeEvent<HTMLInputElement>) => update(item.key, { module: event.target.value })} />,
    },
    {
      title: props.text.moduleConfig,
      dataIndex: "config",
      render: (_, item) => <ModuleConfigInput invalidText={props.text.invalidModuleConfig} item={item} onChange={(patch) => update(item.key, patch)} />,
    },
    {
      title: "",
      key: "actions",
      width: 56,
      render: (_, item) => <Button aria-label={props.text.removeModule} danger icon={<DeleteOutlined />} type="text" onClick={() => props.onChange(props.value.filter((candidate) => candidate.key !== item.key))} />,
    },
  ];
  return <Flex gap="small" vertical>
    <Table<EditorModuleSpec> columns={columns} dataSource={props.value} pagination={false} rowKey="key" scroll={{ x: 960 }} size="small" />
    <Button icon={<PlusOutlined />} onClick={() => props.onChange([...props.value, newModuleSpec()])}>{props.text.addModule}</Button>
  </Flex>;
}
