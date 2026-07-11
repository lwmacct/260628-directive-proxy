import {
  ClearOutlined,
  EyeOutlined,
  ReloadOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import {
  WorkbenchPage,
  WorkbenchPanel,
} from "@lwmacct/260627-antd-workbench";
import {
  Alert,
  Button,
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
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ChangeEvent,
} from "react";
import { apiFetch } from "../../app/auth";
import { useText } from "../../shared/i18n";
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
  const t = useText();
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
      const response = await apiFetch("/api/proxy-exchanges?limit=1000", { signal });
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
      setError(err instanceof Error ? err.message : t.exchanges.requestFailed);
    } finally {
      if (!signal?.aborted) {
        setLoading(false);
      }
    }
  }, [t.exchanges.requestFailed]);

  const updateSettings = useCallback(
    async (enabled: boolean) => {
      setUpdating(true);
      setError(null);
      try {
        const response = await apiFetch("/api/proxy-exchanges/settings", {
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
        setError(err instanceof Error ? err.message : t.exchanges.updateFailed);
      } finally {
        setUpdating(false);
      }
    },
    [capacity, maxBodyBytes, t.exchanges.updateFailed],
  );

  const clearRecords = useCallback(async () => {
    setUpdating(true);
    setError(null);
    try {
      const response = await apiFetch("/api/proxy-exchanges", { method: "DELETE" });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = (await response.json()) as ExchangeSnapshot;
      setSnapshot({ ...emptySnapshot, ...data, items: data.items ?? [] });
      setSelected(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : t.exchanges.clearFailed);
    } finally {
      setUpdating(false);
    }
  }, [t.exchanges.clearFailed]);

  const openRecord = useCallback(async (record: ExchangeRecord) => {
    setSelected(record);
    setDetailLoading(true);
    try {
      const response = await apiFetch(`/api/proxy-exchanges/${record.id}`);
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
      return [item.url, item.target_url, String(item.id)]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(normalizedQuery));
    });
  }, [errorsOnly, method, query, snapshot.items]);

  const columns = useMemo<TableColumnsType<ExchangeRecord>>(
    () => [
      { title: "ID", dataIndex: "id", width: 88, fixed: "left" },
      {
        title: t.exchanges.time,
        dataIndex: "started_at",
        width: 180,
        render: (value: string) => formatDate(value),
      },
      {
        title: t.exchanges.method,
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
        title: t.exchanges.status,
        dataIndex: "status_code",
        width: 108,
        render: (value: number) => (
          <Tag color={statusColor(value)}>{value || t.exchanges.open}</Tag>
        ),
      },
      {
        title: t.exchanges.latency,
        dataIndex: "duration_millis",
        width: 116,
        align: "right",
        render: (value: number) => `${value} ms`,
      },
      {
        title: t.exchanges.body,
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
          <Tooltip title={t.exchanges.details}>
            <Button
              aria-label={t.exchanges.details}
              icon={<EyeOutlined />}
              onClick={() => void openRecord(record)}
              type="text"
            />
          </Tooltip>
        ),
      },
    ],
    [openRecord, t.exchanges],
  );

  return (
    <WorkbenchPage
      description={t.exchanges.description}
      extra={
        <Space wrap>
          <Space className="switch-control">
            <Text type="secondary">{t.exchanges.capture}</Text>
            <Switch
              checked={snapshot.enabled}
              loading={updating}
              onChange={(checked: boolean) => void updateSettings(checked)}
            />
          </Space>
          <Space className="switch-control">
            <Text type="secondary">{t.exchanges.auto}</Text>
            <Switch checked={autoRefresh} onChange={setAutoRefresh} />
          </Space>
          <Button
            icon={<ReloadOutlined />}
            loading={loading}
            onClick={() => void load()}
          >
            {t.exchanges.refresh}
          </Button>
        </Space>
      }
      title={t.app.exchanges}
    >
      <Row gutter={[12, 12]}>
        <Metric label={t.exchanges.retained} value={snapshot.items.length} />
        <Metric label={t.exchanges.capacity} value={snapshot.capacity} />
        <Metric label={t.exchanges.total} value={snapshot.total} />
        <Metric label={t.exchanges.bodyLimit} value={formatBytes(snapshot.max_body_bytes)} />
      </Row>

      <WorkbenchPanel>
        <Flex gap="small" wrap>
          <Input
            allowClear
            className="search-input"
            onChange={(event: ChangeEvent<HTMLInputElement>) =>
              setQuery(event.target.value)
            }
            placeholder={t.exchanges.search}
            prefix={<SearchOutlined />}
            value={query}
          />
          <Select
            allowClear
            className="method-select"
            onChange={setMethod}
            options={allMethods.map((value) => ({ label: value, value }))}
            placeholder={t.exchanges.method}
            value={method}
          />
          <Space className="switch-control">
            <Text type="secondary">{t.exchanges.errors}</Text>
            <Switch checked={errorsOnly} onChange={setErrorsOnly} />
          </Space>
          <InputNumber
            className="number-input"
            min={1}
            max={10000}
            onChange={(value: number | null) =>
              setCapacity(Number(value ?? snapshot.capacity))
            }
            prefix="N"
            value={capacity}
          />
          <InputNumber
            className="number-input"
            min={0}
            max={10485760}
            onChange={(value: number | null) =>
              setMaxBodyBytes(Number(value ?? snapshot.max_body_bytes))
            }
            step={1024}
            value={maxBodyBytes}
          />
          <Button loading={updating} onClick={() => void updateSettings(snapshot.enabled)}>
            {t.exchanges.apply}
          </Button>
          <Popconfirm
            okButtonProps={{ danger: true }}
            okText={t.exchanges.clear}
            onConfirm={() => void clearRecords()}
            title={t.exchanges.clearConfirm}
          >
            <Button danger icon={<ClearOutlined />} loading={updating}>
              {t.exchanges.clear}
            </Button>
          </Popconfirm>
        </Flex>
      </WorkbenchPanel>

      {error ? <Alert title={error} type="error" /> : null}

      <WorkbenchPanel>
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
      </WorkbenchPanel>
      <ExchangeDrawer
        text={t.exchanges}
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
      <WorkbenchPanel>
        <Statistic title={label} value={value} />
      </WorkbenchPanel>
    </Col>
  );
}
