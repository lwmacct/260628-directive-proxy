import {
  ClearOutlined,
  EyeOutlined,
  ReloadOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import { WorkbenchPage } from "@lwmacct/260627-antd-workbench";
import {
  Alert,
  Button,
  Card,
  Empty,
  Flex,
  Input,
  InputNumber,
  Popconfirm,
  Row,
  Col,
  Select,
  Space,
  Statistic,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import type { TableColumnsType } from "antd";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ExchangeDrawer } from "./ExchangeDrawer";
import type { ExchangeRecord, ExchangeSnapshot } from "./types";
import { formatBytes, formatDate, methodColor, statusColor } from "./utils";

const { Text } = Typography;

const emptySnapshot: ExchangeSnapshot = {
  enabled: false,
  capacity: 100,
  max_body_bytes: 65536,
  total: 0,
  items: [],
};

const allMethods = ["GET", "POST", "PUT", "PATCH", "DELETE"];

export function ExchangesPage() {
  const [snapshot, setSnapshot] = useState<ExchangeSnapshot>(emptySnapshot);
  const [loading, setLoading] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<ExchangeRecord | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [query, setQuery] = useState("");
  const [method, setMethod] = useState<string | undefined>();
  const [errorsOnly, setErrorsOnly] = useState(false);
  const [capacity, setCapacity] = useState(emptySnapshot.capacity);
  const [maxBodyBytes, setMaxBodyBytes] = useState(emptySnapshot.max_body_bytes);

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch("/api/proxy-exchanges?limit=1000", { signal });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = (await response.json()) as ExchangeSnapshot;
      const next = {
        ...emptySnapshot,
        ...data,
        items: data.items ?? [],
      };
      setSnapshot(next);
      setCapacity(next.capacity);
      setMaxBodyBytes(next.max_body_bytes);
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

  const updateSettings = useCallback(
    async (enabled: boolean) => {
      setUpdating(true);
      setError(null);
      try {
        const response = await fetch("/api/proxy-exchanges/settings", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            enabled,
            capacity,
            max_body_bytes: maxBodyBytes,
          }),
        });
        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`);
        }
        const data = (await response.json()) as ExchangeSnapshot;
        setSnapshot({ ...emptySnapshot, ...data, items: data.items ?? [] });
      } catch (err) {
        setError(err instanceof Error ? err.message : "Update failed");
      } finally {
        setUpdating(false);
      }
    },
    [capacity, maxBodyBytes],
  );

  const clearRecords = useCallback(async () => {
    setUpdating(true);
    setError(null);
    try {
      const response = await fetch("/api/proxy-exchanges", { method: "DELETE" });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = (await response.json()) as ExchangeSnapshot;
      setSnapshot({ ...emptySnapshot, ...data, items: data.items ?? [] });
      setSelected(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Clear failed");
    } finally {
      setUpdating(false);
    }
  }, []);

  const openRecord = useCallback(async (record: ExchangeRecord) => {
    setSelected(record);
    setDetailLoading(true);
    try {
      const response = await fetch(`/api/proxy-exchanges/${record.id}`);
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      setSelected((await response.json()) as ExchangeRecord);
    } catch {
      setSelected(record);
    } finally {
      setDetailLoading(false);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    if (!autoRefresh) {
      return undefined;
    }
    const timer = window.setInterval(() => {
      void load();
    }, 5000);
    return () => window.clearInterval(timer);
  }, [autoRefresh, load]);

  const filteredItems = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return snapshot.items.filter((item) => {
      if (method && item.method !== method) {
        return false;
      }
      if (errorsOnly && item.status_code < 400) {
        return false;
      }
      if (!normalizedQuery) {
        return true;
      }
      return [item.url, item.target_url, item.request_id, String(item.id)]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(normalizedQuery));
    });
  }, [errorsOnly, method, query, snapshot.items]);

  const columns = useMemo<TableColumnsType<ExchangeRecord>>(
    () => [
      { title: "ID", dataIndex: "id", width: 88, fixed: "left" },
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
          <Tooltip title="Details">
            <Button
              aria-label="Details"
              icon={<EyeOutlined />}
              onClick={() => void openRecord(record)}
              type="text"
            />
          </Tooltip>
        ),
      },
    ],
    [openRecord],
  );

  return (
    <WorkbenchPage
      description="查看、过滤和管理最近的代理请求响应记录。"
      extra={
        <Space wrap>
          <Space className="switch-control">
            <Text type="secondary">Capture</Text>
            <Switch
              checked={snapshot.enabled}
              loading={updating}
              onChange={(checked) => void updateSettings(checked)}
            />
          </Space>
          <Space className="switch-control">
            <Text type="secondary">Auto</Text>
            <Switch checked={autoRefresh} onChange={setAutoRefresh} />
          </Space>
          <Button
            icon={<ReloadOutlined />}
            loading={loading}
            onClick={() => void load()}
          >
            Refresh
          </Button>
        </Space>
      }
      title="请求记录"
    >
      <Row gutter={[12, 12]} style={{ marginBottom: 16 }}>
        <Metric label="Retained" value={snapshot.items.length} />
        <Metric label="Capacity" value={snapshot.capacity} />
        <Metric label="Total" value={snapshot.total} />
        <Metric label="Body Limit" value={formatBytes(snapshot.max_body_bytes)} />
      </Row>

      <Card size="small" style={{ marginBottom: 16 }}>
        <Flex gap="small" wrap>
        <Input
          allowClear
          className="search-input"
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search URL, target, request ID, ID"
          prefix={<SearchOutlined />}
          value={query}
        />
        <Select
          allowClear
          className="method-select"
          onChange={setMethod}
          options={allMethods.map((value) => ({ label: value, value }))}
          placeholder="Method"
          value={method}
        />
        <Space className="switch-control">
          <Text type="secondary">Errors</Text>
          <Switch checked={errorsOnly} onChange={setErrorsOnly} />
        </Space>
        <InputNumber
          className="number-input"
          min={1}
          max={10000}
          onChange={(value) => setCapacity(Number(value ?? snapshot.capacity))}
          prefix="N"
          value={capacity}
        />
        <InputNumber
          className="number-input"
          min={0}
          max={10485760}
          onChange={(value) =>
            setMaxBodyBytes(Number(value ?? snapshot.max_body_bytes))
          }
          step={1024}
          value={maxBodyBytes}
        />
        <Button loading={updating} onClick={() => void updateSettings(snapshot.enabled)}>
          Apply
        </Button>
        <Popconfirm
          okButtonProps={{ danger: true }}
          okText="Clear"
          onConfirm={() => void clearRecords()}
          title="Clear retained records?"
        >
          <Button danger icon={<ClearOutlined />} loading={updating}>
            Clear
          </Button>
        </Popconfirm>
        </Flex>
      </Card>

      {error ? <Alert className="status-alert" message={error} type="error" /> : null}

      <Table<ExchangeRecord>
        columns={columns}
        dataSource={filteredItems}
        loading={loading}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
        pagination={{ pageSize: 20, showSizeChanger: true }}
        rowKey="id"
        scroll={{ x: 1120 }}
        size="middle"
      />
      <ExchangeDrawer
        loading={detailLoading}
        record={selected}
        onClose={() => setSelected(null)}
      />
    </WorkbenchPage>
  );
}

function Metric({ label, value }: { label: string; value: number | string }) {
  return (
    <Col xs={24} sm={12} lg={6}>
      <Card size="small">
        <Statistic title={label} value={value} />
      </Card>
    </Col>
  );
}
