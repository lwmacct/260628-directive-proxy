import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { Button, Checkbox, Col, Divider, Flex, Form, Input, InputNumber, Row, Switch, Typography } from "antd";
import type { CheckboxChangeEvent } from "antd/es/checkbox";
import type { ChangeEvent } from "react";
import type { Text } from "../../../shared/i18n";
import { newStatusRange } from "../constants";
import type { RecoveryEditorState } from "../types";
import { KeyValueEditor } from "./KeyValueEditor";

const { Text: Label } = Typography;

export function RecoveryEditor(props: {
  text: Text["authConsole"];
  value: RecoveryEditorState;
  onChange: (value: RecoveryEditorState) => void;
}) {
  const { text, value } = props;
  const update = (patch: Partial<RecoveryEditorState>) => props.onChange({ ...value, ...patch });
  const updateRange = (key: string, patch: { from?: number; to?: number }) => {
    update({ expectedStatuses: value.expectedStatuses.map((item) => item.key === key ? { ...item, ...patch } : item) });
  };
  return <Flex gap="middle" vertical>
    <Flex align="center" gap="small" justify="space-between" wrap>
      <div>
        <Label strong>{text.recoveryController}</Label><br />
        <Label type="secondary">{text.recoveryDescription}</Label>
      </div>
      <Switch checked={value.enabled} checkedChildren={text.enabled} unCheckedChildren={text.disabled} onChange={(enabled: boolean) => update({ enabled })} />
    </Flex>
    {value.enabled ? <>
      <section className="builder-card">
        <Label strong>{text.controller}</Label>
        <Divider />
        <Row gutter={[16, 0]}>
          <Col xs={24} lg={16}><Form.Item label={text.controllerURL}><Input placeholder="https://controller.example.com/recovery" value={value.controllerURL} onChange={(event: ChangeEvent<HTMLInputElement>) => update({ controllerURL: event.target.value })} /></Form.Item></Col>
          <Col xs={24} lg={8}><Form.Item label={text.controllerTimeout}><Input placeholder="3s" value={value.controllerTimeout} onChange={(event: ChangeEvent<HTMLInputElement>) => update({ controllerTimeout: event.target.value })} /></Form.Item></Col>
        </Row>
        <Form.Item label={text.controllerHeaders} style={{ marginBottom: 0 }}>
          <KeyValueEditor addLabel={text.addControllerHeader} items={value.controllerHeaders} removeLabel={text.removeControllerHeader} onChange={(controllerHeaders) => update({ controllerHeaders })} />
        </Form.Item>
      </section>
      <section className="builder-card">
        <Label strong>{text.triggers}</Label>
        <Divider />
        <Row gutter={[16, 0]}>
          <Col xs={24} lg={12}><Form.Item label={text.responseHeaderTimeout}><Input allowClear placeholder="10s" value={value.responseHeaderTimeout} onChange={(event: ChangeEvent<HTMLInputElement>) => update({ responseHeaderTimeout: event.target.value })} /></Form.Item></Col>
          <Col xs={24} lg={12}><Form.Item label={text.errorTriggers}>
            <Flex gap="large" wrap>
              <Checkbox checked={value.transportError} onChange={(event: CheckboxChangeEvent) => update({ transportError: event.target.checked })}>{text.transportError}</Checkbox>
              <Checkbox checked={value.directiveError} onChange={(event: CheckboxChangeEvent) => update({ directiveError: event.target.checked })}>{text.directiveError}</Checkbox>
            </Flex>
          </Form.Item></Col>
        </Row>
        <Divider />
        <Flex align="center" gap="small" justify="space-between" style={{ marginBottom: 12 }} wrap>
          <div>
            <Label strong>{text.unexpectedStatus}</Label><br />
            <Label type="secondary">{text.expectedStatusDescription}</Label>
          </div>
          <Switch checked={value.unexpectedStatusEnabled} onChange={(unexpectedStatusEnabled: boolean) => update({ unexpectedStatusEnabled })} />
        </Flex>
        {value.unexpectedStatusEnabled ? <>
          <Flex gap="small" vertical>
            {value.expectedStatuses.map((range) => <Flex align="center" gap="small" key={range.key} wrap>
              <InputNumber aria-label={text.rangeFrom} className="status-number" max={599} min={200} value={range.from} onChange={(from: number | null) => updateRange(range.key, { from: from ?? 200 })} />
              <Label type="secondary">—</Label>
              <InputNumber aria-label={text.rangeTo} className="status-number" max={599} min={200} value={range.to} onChange={(to: number | null) => updateRange(range.key, { to: to ?? 599 })} />
              <Button aria-label={text.removeStatusRange} danger icon={<DeleteOutlined />} type="text" onClick={() => update({ expectedStatuses: value.expectedStatuses.filter((item) => item.key !== range.key) })} />
            </Flex>)}
            <Button icon={<PlusOutlined />} onClick={() => update({ expectedStatuses: [...value.expectedStatuses, newStatusRange()] })}>{text.addStatusRange}</Button>
          </Flex>
          <Form.Item label={text.captureBodyBytes} style={{ marginBottom: 0, marginTop: 16 }}>
            <InputNumber min={1} max={16 << 20} value={value.captureBodyBytes} onChange={(captureBodyBytes: number | null) => update({ captureBodyBytes: captureBodyBytes ?? undefined })} />
          </Form.Item>
        </> : null}
      </section>
      <section className="builder-card">
        <Label strong>{text.budget}</Label>
        <Divider />
        <Row gutter={[16, 0]}>
          <Col xs={24} md={12}><Form.Item label={text.maxAttempts} style={{ marginBottom: 0 }}><InputNumber min={1} max={100} value={value.maxAttempts} onChange={(maxAttempts: number | null) => update({ maxAttempts: maxAttempts ?? 1 })} /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item label={text.maxElapsed} style={{ marginBottom: 0 }}><Input placeholder="30s" value={value.maxElapsed} onChange={(event: ChangeEvent<HTMLInputElement>) => update({ maxElapsed: event.target.value })} /></Form.Item></Col>
        </Row>
      </section>
    </> : null}
  </Flex>;
}
