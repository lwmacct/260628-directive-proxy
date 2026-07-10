import { WorkbenchPanel } from "@lwmacct/260627-antd-workbench";
import { Descriptions, Drawer, Empty, Space, Tag, Typography } from "antd";
import type { BodySnapshot, ExchangeRecord } from "./types";
import { formatBytes, formatDate, methodColor, statusColor } from "./utils";

const { Paragraph, Text } = Typography;

export function ExchangeDrawer({
  loading,
  record,
  onClose,
}: {
  loading: boolean;
  record: ExchangeRecord | null;
  onClose: () => void;
}) {
  return (
    <Drawer
      destroyOnHidden
      loading={loading}
      onClose={onClose}
      open={record != null}
      size="large"
      title={record ? `Exchange #${record.id}` : ""}
    >
      {record ? (
        <Space className="drawer-stack" orientation="vertical" size={18}>
          <Descriptions bordered column={1} size="small">
            <Descriptions.Item label="Started">
              {formatDate(record.started_at)}
            </Descriptions.Item>
            <Descriptions.Item label="Duration">
              {record.duration_millis} ms
            </Descriptions.Item>
            <Descriptions.Item label="Method">
              <Tag color={methodColor(record.method)}>{record.method}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Status">
              <Tag color={statusColor(record.status_code)}>
                {record.status_code}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="URL">
              <Text copyable>{record.url}</Text>
            </Descriptions.Item>
            <Descriptions.Item label="Target">
              <Text copyable>{record.target_url || "-"}</Text>
            </Descriptions.Item>
          </Descriptions>

          <BodyBlock title="Request Body" body={record.request_body} />
          <BodyBlock title="Response Body" body={record.response_body} />
          <JSONBlock title="Request Headers" value={record.request_headers} />
          <JSONBlock title="Response Headers" value={record.response_headers} />
        </Space>
      ) : null}
    </Drawer>
  );
}

function BodyBlock({ title, body }: { title: string; body: BodySnapshot }) {
  const content = body.text ?? body.base64 ?? "";
  return (
    <WorkbenchPanel
      extra={
        <Text type="secondary">
          {formatBytes(body.bytes)}
          {body.truncated ? ` / ${formatBytes(body.captured_bytes)} captured` : ""}
        </Text>
      }
      title={title}
    >
      {content ? (
        <Paragraph className="code-output" copyable>
          {content}
        </Paragraph>
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
      )}
    </WorkbenchPanel>
  );
}

function JSONBlock({
  title,
  value,
}: {
  title: string;
  value?: Record<string, string[]>;
}) {
  return (
    <WorkbenchPanel title={title}>
      {value ? (
        <Paragraph className="code-output" copyable>
          {JSON.stringify(value, null, 2)}
        </Paragraph>
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
      )}
    </WorkbenchPanel>
  );
}
