import { DeleteOutlined } from "@ant-design/icons";
import { Button, Input, Segmented, Select, Table } from "antd";
import type { TableColumnsType } from "antd";
import type { ChangeEvent } from "react";
import type { HeaderOp } from "../types";
import type { Text } from "../../../shared/i18n";

export function HeaderOperationsTable(props: { items: HeaderOp[]; text: Text["authConsole"]; onChange: (key: string, patch: Partial<HeaderOp>) => void; onRemove: (key: string) => void }) {
  const columns: TableColumnsType<HeaderOp> = [
    { title: props.text.op, dataIndex: "op", width: 104, render: (_, record) => <Select disabled={record.selector === "preset"} options={record.selector === "preset" ? [{ label: props.text.remove, value: "-" }] : [{ label: props.text.set, value: "=" }, { label: "Add", value: "+" }, { label: props.text.remove, value: "-" }]} value={record.op} onChange={(op: HeaderOp["op"]) => props.onChange(record.key, { op })} /> },
    { title: props.text.match, dataIndex: "selector", width: 220, render: (_, record) => <Segmented options={[{ label: props.text.exact, value: "name" }, { label: "Glob", value: "glob" }, { label: props.text.preset, value: "preset" }]} value={record.selector} onChange={(selector: HeaderOp["selector"]) => props.onChange(record.key, selector === "preset" ? { selector, op: "-", pattern: "proxy-disclosure", values: [] } : { selector, pattern: "" })} /> },
    { title: props.text.selector, dataIndex: "pattern", render: (_, record) => record.selector === "preset" ? <Select options={[{ label: "proxy-disclosure", value: "proxy-disclosure" }]} style={{ width: "100%" }} value={record.pattern} onChange={(pattern: string) => props.onChange(record.key, { pattern })} /> : <Input placeholder={record.selector === "glob" ? "X-Tenant-*" : "Authorization"} value={record.pattern} onChange={(event: ChangeEvent<HTMLInputElement>) => props.onChange(record.key, { pattern: event.target.value })} /> },
    { title: props.text.values, dataIndex: "values", render: (_, record) => record.op === "-" ? null : <Select mode="tags" open={false} placeholder={props.text.valuePlaceholder} style={{ width: "100%" }} value={record.values} onChange={(values: string[]) => props.onChange(record.key, { values })} /> },
    { title: "", key: "actions", width: 56, render: (_, record) => <Button aria-label={props.text.removeHeaderOp} icon={<DeleteOutlined />} onClick={() => props.onRemove(record.key)} type="text" /> },
  ];
  return <Table<HeaderOp> columns={columns} dataSource={props.items} pagination={false} rowKey="key" scroll={{ x: 920 }} size="small" />;
}
