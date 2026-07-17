import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { Button, Flex, Input } from "antd";
import type { ChangeEvent } from "react";
import { newResolverHeader } from "../constants";
import type { ResolverHeader } from "../types";

export function KeyValueEditor(props: {
  addLabel: string;
  items: ResolverHeader[];
  namePlaceholder?: string;
  removeLabel: string;
  valuePlaceholder?: string;
  onChange: (items: ResolverHeader[]) => void;
}) {
  const update = (key: string, patch: Partial<ResolverHeader>) => {
    props.onChange(props.items.map((item) => item.key === key ? { ...item, ...patch } : item));
  };
  return <Flex gap="small" vertical>
    {props.items.map((item) => <Flex gap="small" key={item.key} wrap>
      <Input
        className="key-value-name"
        placeholder={props.namePlaceholder ?? "Authorization"}
        value={item.name}
        onChange={(event: ChangeEvent<HTMLInputElement>) => update(item.key, { name: event.target.value })}
      />
      <Input
        className="key-value-value"
        placeholder={props.valuePlaceholder ?? "Bearer token"}
        value={item.value}
        onChange={(event: ChangeEvent<HTMLInputElement>) => update(item.key, { value: event.target.value })}
      />
      <Button
        aria-label={props.removeLabel}
        danger
        icon={<DeleteOutlined />}
        type="text"
        onClick={() => props.onChange(props.items.filter((candidate) => candidate.key !== item.key))}
      />
    </Flex>)}
    <Button icon={<PlusOutlined />} onClick={() => props.onChange([...props.items, newResolverHeader("", "")])}>
      {props.addLabel}
    </Button>
  </Flex>;
}
