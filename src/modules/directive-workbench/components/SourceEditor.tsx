import { Input } from "antd";
import type { ChangeEvent } from "react";

export function SourceEditor(props: { placeholder: string; value: string; onChange: (value: string) => void }) {
  return <Input.TextArea autoSize={{ minRows: 10 }} className="source-input" onChange={(event: ChangeEvent<HTMLTextAreaElement>) => props.onChange(event.target.value)} placeholder={props.placeholder} value={props.value} />;
}
