import { EyeOutlined, ReloadOutlined } from "@ant-design/icons";
import {
  Alert,
  Button,
  ConfigProvider,
  Descriptions,
  Drawer,
  Empty,
  Layout,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import type { TableColumnsType } from "antd";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "antd/dist/reset.css";
import "./styles/global.css";

const { Header, Content } = Layout;
const { Text, Title } = Typography;

type BodySnapshot = {
  text?: string;
  base64?: string;
  bytes: number;
  captured_bytes: number;
  truncated: boolean;
};

type ExchangeRecord = {
  id: number;
  request_id?: string;
  started_at: string;
  completed_at: string;
  duration_millis: number;
  method: string;
  host?: string;
  url: string;
  target_url?: string;
  status_code: number;
  request_headers?: Record<string, string[]>;
  response_headers?: Record<string, string[]>;
  request_body: BodySnapshot;
  response_body: BodySnapshot;
};

type ExchangeSnapshot = {
  enabled: boolean;
  capacity: number;
  total: number;
  items: ExchangeRecord[];
};

const emptySnapshot: ExchangeSnapshot = {
  enabled: false,
  capacity: 0,
  total: 0,
  items: [],
};

function App() {
  const [snapshot, setSnapshot] = useState<ExchangeSnapshot>(emptySnapshot);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<ExchangeRecord | null>(null);

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch("/api/proxy-exchanges", { signal });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = (await response.json()) as ExchangeSnapshot;
      setSnapshot({
        ...data,
        items: data.items ?? [],
      });
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") {
        return;
      }
      setError(err instanceof Error ? err.message : "Request failed");
    } finally {
      if (!signal?.aborted) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    const timer = window.setInterval(() => {
      void load(controller.signal);
    }, 5000);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [load]);

  const columns = useMemo<TableColumnsType<ExchangeRecord>>(
    () => [
      {
        title: "ID",
        dataIndex: "id",
        width: 88,
        fixed: "left",
      },
      {
        title: "Time",
        dataIndex: "started_at",
        width: 180,
        render: (value: string) => formatDate(value),
      },
      {
        title: "Method",
        dataIndex: "method",
        width: 104,
        render: (value: string) => <Tag color={methodColor(value)}>{value}</Tag>,
      },
      {
        title: "URL",
        dataIndex: "url",
        ellipsis: true,
        render: (value: string) => <Text copyable>{value}</Text>,
      },
      {
        title: "Status",
        dataIndex: "status_code",
        width: 108,
        render: (value: number) => (
          <Tag color={statusColor(value)}>{value || "open"}</Tag>
        ),
      },
      {
        title: "Latency",
        dataIndex: "duration_millis",
        width: 116,
        align: "right",
        render: (value: number) => `${value} ms`,
      },
      {
        title: "Body",
        key: "body",
        width: 140,
        align: "right",
        render: (_, record) =>
          formatBytes(record.request_body.bytes + record.response_body.bytes),
      },
      {
        title: "",
        key: "actions",
        width: 64,
        fixed: "right",
        render: (_, record) => (
          <Button
            aria-label="Details"
            icon={<EyeOutlined />}
            onClick={() => setSelected(record)}
            type="text"
          />
        ),
      },
    ],
    [],
  );

  return (
    <ConfigProvider
      theme={{
        token: {
          borderRadius: 6,
          colorPrimary: "#246bfe",
          fontFamily:
            'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
        },
      }}
    >
      <Layout className="app-shell">
        <Header className="app-header">
          <div>
            <Title level={3}>Proxy Exchanges</Title>
            <Text type="secondary">LLM Relay DProxy</Text>
          </div>
          <Space>
            <Tag color={snapshot.enabled ? "green" : "default"}>
              {snapshot.enabled ? "enabled" : "disabled"}
            </Tag>
            <Button
              icon={<ReloadOutlined />}
              loading={loading}
              onClick={() => void load()}
            >
              Refresh
            </Button>
          </Space>
        </Header>
        <Content className="app-content">
          <section className="metrics-grid">
            <Metric label="Retained" value={snapshot.items.length} />
            <Metric label="Capacity" value={snapshot.capacity} />
            <Metric label="Total" value={snapshot.total} />
          </section>

          {error ? (
            <Alert className="status-alert" message={error} type="error" />
          ) : null}

          <Table<ExchangeRecord>
            className="exchange-table"
            columns={columns}
            dataSource={snapshot.items}
            loading={loading}
            locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
            pagination={{ pageSize: 20, showSizeChanger: true }}
            rowKey="id"
            scroll={{ x: 1120 }}
            size="middle"
          />
        </Content>
      </Layout>
      <ExchangeDrawer record={selected} onClose={() => setSelected(null)} />
    </ConfigProvider>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="metric-tile">
      <Text type="secondary">{label}</Text>
      <strong>{value.toLocaleString()}</strong>
    </div>
  );
}

function ExchangeDrawer({
  record,
  onClose,
}: {
  record: ExchangeRecord | null;
  onClose: () => void;
}) {
  return (
    <Drawer
      destroyOnHidden
      onClose={onClose}
      open={record != null}
      title={record ? `Exchange #${record.id}` : ""}
      width="min(100vw, 860px)"
    >
      {record ? (
        <Space className="drawer-stack" direction="vertical" size={18}>
          <Descriptions bordered column={1} size="small">
            <Descriptions.Item label="Request ID">
              {record.request_id || "-"}
            </Descriptions.Item>
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
    <section className="detail-block">
      <div className="detail-block-title">
        <Text strong>{title}</Text>
        <Text type="secondary">
          {formatBytes(body.bytes)}
          {body.truncated ? ` / ${formatBytes(body.captured_bytes)} captured` : ""}
        </Text>
      </div>
      {content ? <pre>{content}</pre> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />}
    </section>
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
    <section className="detail-block">
      <div className="detail-block-title">
        <Text strong>{title}</Text>
      </div>
      {value ? (
        <pre>{JSON.stringify(value, null, 2)}</pre>
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
      )}
    </section>
  );
}

function formatDate(value: string) {
  if (!value) {
    return "-";
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "short",
    timeStyle: "medium",
  }).format(new Date(value));
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }
  const units = ["B", "KiB", "MiB", "GiB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function methodColor(method: string) {
  switch (method) {
    case "GET":
      return "blue";
    case "POST":
      return "green";
    case "DELETE":
      return "red";
    case "PATCH":
    case "PUT":
      return "orange";
    default:
      return "default";
  }
}

function statusColor(status: number) {
  if (status >= 500) {
    return "red";
  }
  if (status >= 400) {
    return "orange";
  }
  if (status >= 300) {
    return "blue";
  }
  if (status >= 200) {
    return "green";
  }
  return "default";
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
