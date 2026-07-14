import {
  ReloadOutlined,
  RetweetOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import {
  WorkbenchPage,
  WorkbenchPanel,
} from "@lwmacct/260627-antd-workbench";
import {
  Alert,
  Button,
  Col,
  Empty,
  Flex,
  Input,
  Popconfirm,
  Row,
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
import type {
  ActiveProxyRequest,
  ActiveProxyRequestSnapshot,
} from "./types";
import { formatDate, methodColor } from "./utils";

const { Text } = Typography;

const emptySnapshot: ActiveProxyRequestSnapshot = {
  server_time: "",
  items: [],
};

export function ExchangesPage() {
  const t = useText();
  const [snapshot, setSnapshot] = useState(emptySnapshot);
  const [loading, setLoading] = useState(false);
  const [retrying, setRetrying] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [query, setQuery] = useState("");

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    setError(null);
    try {
      const response = await apiFetch(
        "/api/control/proxy-requests",
        { signal },
      );
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = (await response.json()) as ActiveProxyRequestSnapshot;
      setSnapshot({ ...emptySnapshot, ...data, items: data.items ?? [] });
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

  const retry = useCallback(async (request: ActiveProxyRequest) => {
    setRetrying(request.trace_id);
    setError(null);
    try {
      const response = await apiFetch(
        `/api/control/proxy-requests/${request.trace_id}/retries`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ expected_attempt: request.attempt }),
        },
      );
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t.exchanges.retryFailed);
    } finally {
      setRetrying(null);
    }
  }, [load, t.exchanges.retryFailed]);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    if (!autoRefresh) {
      return undefined;
    }
    const timer = window.setInterval(() => void load(), 2000);
    return () => window.clearInterval(timer);
  }, [autoRefresh, load]);

  const filteredItems = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) {
      return snapshot.items;
    }
    return snapshot.items.filter((item) =>
      [
        item.trace_id,
        item.url,
        item.target_url,
        ...Object.entries(item.metadata ?? {}).flatMap(([name, values]) => [name, ...values]),
      ]
        .some((value) => value.toLowerCase().includes(normalized)),
    );
  }, [query, snapshot.items]);

  const retryableCount = snapshot.items.filter((item) => item.retryable).length;
  const oldestWait = snapshot.items.reduce(
    (value, item) => Math.max(value, item.waiting_millis),
    0,
  );

  const columns = useMemo<TableColumnsType<ActiveProxyRequest>>(() => [
    {
      title: "Trace ID",
      dataIndex: "trace_id",
      width: 280,
      fixed: "left",
      render: (value: string) => <Text copyable code>{value}</Text>,
    },
    {
      title: t.exchanges.time,
      dataIndex: "attempt_started_at",
      width: 180,
      render: (value: string) => formatDate(value),
    },
    {
      title: t.exchanges.metadata,
      dataIndex: "metadata",
      width: 320,
      ellipsis: true,
      render: (value: ActiveProxyRequest["metadata"]) => (
        <Text code>{JSON.stringify(value ?? {})}</Text>
      ),
    },
    {
      title: t.exchanges.method,
      dataIndex: "method",
      width: 100,
      render: (value: string) => <Tag color={methodColor(value)}>{value}</Tag>,
    },
    { title: "URL", dataIndex: "url", width: 300, ellipsis: true },
    {
      title: t.exchanges.target,
      dataIndex: "target_url",
      width: 300,
      ellipsis: true,
    },
    {
      title: t.exchanges.attempt,
      key: "attempt",
      width: 110,
      align: "right",
      render: (_, item) => `${item.attempt}/${item.max_attempts}`,
    },
    {
      title: t.exchanges.waiting,
      dataIndex: "waiting_millis",
      width: 130,
      align: "right",
      render: (value: number) => `${(value / 1000).toFixed(1)} s`,
    },
    {
      title: t.exchanges.status,
      dataIndex: "state",
      width: 170,
      render: (value: ActiveProxyRequest["state"]) => {
        const labels = {
          resolving_directive: t.exchanges.resolving,
          buffering_body: t.exchanges.buffering,
          awaiting_response: t.exchanges.awaiting,
          retry_requested: t.exchanges.retrying,
        };
        const colors = {
          resolving_directive: "blue",
          buffering_body: "cyan",
          awaiting_response: "warning",
          retry_requested: "processing",
        };
        return <Tag color={colors[value]}>{labels[value]}</Tag>;
      },
    },
    {
      title: "",
      key: "actions",
      width: 76,
      fixed: "right",
      render: (_, item) => (
        <Popconfirm
          disabled={!item.retryable}
          title={t.exchanges.retryConfirm}
          onConfirm={() => void retry(item)}
        >
          <Tooltip title={item.retryable ? t.exchanges.retry : t.exchanges.retryNotReady}>
            <Button
              aria-label={t.exchanges.retry}
              disabled={!item.retryable}
              icon={<RetweetOutlined />}
              loading={retrying === item.trace_id}
              type="text"
            />
          </Tooltip>
        </Popconfirm>
      ),
    },
  ], [retry, retrying, t.exchanges]);

  return (
    <WorkbenchPage
      description={t.exchanges.description}
      extra={
        <Space wrap>
          <Space className="switch-control">
            <Text type="secondary">{t.exchanges.auto}</Text>
            <Switch checked={autoRefresh} onChange={setAutoRefresh} />
          </Space>
          <Button icon={<ReloadOutlined />} loading={loading} onClick={() => void load()}>
            {t.exchanges.refresh}
          </Button>
        </Space>
      }
      title={t.app.exchanges}
    >
      <Row gutter={[12, 12]}>
        <Metric label={t.exchanges.active} value={snapshot.items.length} />
        <Metric label={t.exchanges.retryable} value={retryableCount} />
        <Metric label={t.exchanges.oldestWait} value={`${(oldestWait / 1000).toFixed(1)} s`} />
      </Row>

      <WorkbenchPanel>
        <Flex gap="small" wrap>
          <Input
            allowClear
            className="search-input"
            onChange={(event: ChangeEvent<HTMLInputElement>) => setQuery(event.target.value)}
            placeholder={t.exchanges.search}
            prefix={<SearchOutlined />}
            value={query}
          />
        </Flex>
      </WorkbenchPanel>

      {error ? <Alert title={error} type="error" /> : null}

      <WorkbenchPanel>
        <Table<ActiveProxyRequest>
          columns={columns}
          dataSource={filteredItems}
          loading={loading}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
          pagination={{ pageSize: 20, showSizeChanger: true }}
          rowKey="trace_id"
          scroll={{ x: 1880 }}
          size="middle"
        />
      </WorkbenchPanel>
    </WorkbenchPage>
  );
}

function Metric({ label, value }: { label: string; value: number | string }) {
  return (
    <Col xs={24} sm={12} lg={8}>
      <WorkbenchPanel>
        <Statistic title={label} value={value} />
      </WorkbenchPanel>
    </Col>
  );
}
