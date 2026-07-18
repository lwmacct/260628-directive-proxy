import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { Button, Checkbox, Col, Divider, Flex, Form, Input, InputNumber, Row, Switch, Typography } from "antd";
import type { CheckboxChangeEvent } from "antd/es/checkbox";
import type { ChangeEvent } from "react";
import type { Text } from "../../../shared/i18n";
import { newStatusRange } from "../constants";
import type { RecoveryEditorState } from "../types";

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
  const updateControllerConfig = (controllerConfigText: string) => {
    try {
      update({ controllerConfigText, controllerConfig: JSON.parse(controllerConfigText || "{}") as unknown, controllerConfigValid: true });
    } catch {
      update({ controllerConfigText, controllerConfigValid: false });
    }
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
        <Form.Item label={text.controllerModule}>
          <Input placeholder="builtin.recovery" value={value.controllerModule} onChange={(event: ChangeEvent<HTMLInputElement>) => update({ controllerModule: event.target.value })} />
        </Form.Item>
        <Form.Item label={text.controllerConfig} style={{ marginBottom: 0 }}>
          <Input.TextArea
            autoSize={{ minRows: 4, maxRows: 12 }}
            className="source-input"
            status={value.controllerConfigValid ? undefined : "error"}
            value={value.controllerConfigText}
            onChange={(event: ChangeEvent<HTMLTextAreaElement>) => updateControllerConfig(event.target.value)}
          />
          {!value.controllerConfigValid ? <Label type="danger">{text.invalidControllerConfig}</Label> : null}
        </Form.Item>
      </section>
      <section className="builder-card">
        <Label strong>{text.triggers}</Label>
        <Divider />
        <Row gutter={[16, 0]}>
          <Col xs={24} lg={12}><Form.Item label={text.responseHeaderTimeout}><Input allowClear placeholder="10s" value={value.responseHeaderTimeout} onChange={(event: ChangeEvent<HTMLInputElement>) => update({ responseHeaderTimeout: event.target.value })} /></Form.Item></Col>
          <Col xs={24} lg={12}><Form.Item label={text.errorTriggers}><Checkbox checked={value.transportError} onChange={(event: CheckboxChangeEvent) => update({ transportError: event.target.checked })}>{text.transportError}</Checkbox></Form.Item></Col>
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
          <Col xs={24} md={12}><Form.Item label={text.maxRoundTrips} style={{ marginBottom: 0 }}><InputNumber min={1} max={100} value={value.maxRoundTrips} onChange={(maxRoundTrips: number | null) => update({ maxRoundTrips: maxRoundTrips ?? 1 })} /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item label={text.maxElapsed} style={{ marginBottom: 0 }}><Input placeholder="30s" value={value.maxElapsed} onChange={(event: ChangeEvent<HTMLInputElement>) => update({ maxElapsed: event.target.value })} /></Form.Item></Col>
        </Row>
      </section>
    </> : null}
  </Flex>;
}
