package config

import (
	"errors"
	"strings"
	"time"
)

const DefaultEventQueueSize = 10_000

var (
	ErrInvalidHTTP      = errors.New("invalid http config")
	ErrInvalidProxy     = errors.New("invalid proxy config")
	ErrInvalidKafka     = errors.New("invalid kafka config")
	ErrInvalidTransport = errors.New("invalid transport config")
	ErrInvalidUsage     = errors.New("invalid usage plugin config")
)

type Config struct {
	Server  Server  `json:"server"  desc:"服务运行配置"`
	Proxy   Proxy   `json:"proxy"   desc:"代理 data plane 配置"`
	Event   Event   `json:"event"   desc:"事件投递配置"`
	Plugins Plugins `json:"plugins" desc:"插件配置"`
}

type Server struct {
	Debug bool       `json:"debug" desc:"启用调试日志和诊断信息"`
	HTTP  ServerHTTP `json:"http"  desc:"HTTP 服务配置"`
}

type ServerHTTP struct {
	Listen          string        `json:"listen"             desc:"HTTP 服务监听地址"`
	TLS             ServerHTTPTLS `json:"tls"                desc:"HTTPS TLS 配置"`
	ReadTimeout     time.Duration `json:"read-timeout"       desc:"HTTP 读取超时时间"`
	WriteTimeout    time.Duration `json:"write-timeout"      desc:"HTTP 写入超时时间；代理流式响应建议保持 0"`
	IdleTimeout     time.Duration `json:"idle-timeout"       desc:"HTTP 空闲连接超时时间"`
	MaxAPIBodyBytes int64         `json:"max-api-body-bytes" desc:"Control plane API 最大请求体字节数，0 表示不限制"`
}

type ServerHTTPTLS struct {
	Enabled        bool          `json:"enabled"         desc:"是否启用 HTTPS TLS"`
	CertFile       string        `json:"cert-file"       desc:"TLS 证书文件路径"`
	KeyFile        string        `json:"key-file"        desc:"TLS 私钥文件路径"`
	AutoReload     bool          `json:"auto-reload"     desc:"是否自动重载 TLS 证书文件"`
	ReloadInterval time.Duration `json:"reload-interval" desc:"TLS 证书文件自动重载检查间隔"`
}

type Proxy struct {
	PathPrefix string         `json:"path-prefix" desc:"代理流量路径前缀"`
	Transport  ProxyTransport `json:"transport"   desc:"上游连接池与连接复用配置"`
}

type ProxyTransport struct {
	MaxIdleConns        int           `json:"max-idle-conns"         desc:"全局空闲连接池容量；只影响连接复用，不限制活跃并发"`
	MaxIdleConnsPerHost int           `json:"max-idle-conns-per-host" desc:"单 upstream 空闲连接池容量；只影响连接复用，不限制活跃并发"`
	MaxConnsPerHost     int           `json:"max-conns-per-host"      desc:"单 upstream 活跃连接上限；0 表示不限制"`
	IdleConnTimeout     time.Duration `json:"idle-conn-timeout"       desc:"空闲连接在连接池中的保留时间"`
	DisableKeepAlives   bool          `json:"disable-keep-alives"    desc:"是否禁用上游 keep-alive"`
}

type Event struct {
	Kafka Kafka `json:"kafka" desc:"Kafka 事件投递配置"`
}

type Kafka struct {
	Enabled           bool          `json:"enabled"             desc:"是否启用 Kafka"`
	CaptureAbnormal   bool          `json:"capture-abnormal"    desc:"Kafka 启用时是否自动采集异常请求和响应"`
	EnsureTopics      bool          `json:"ensure-topics"       desc:"启动时是否尝试创建 topic"`
	Brokers           string        `json:"brokers"             desc:"Kafka broker 地址列表，逗号分隔"`
	TopicPrefix       string        `json:"topic-prefix"        desc:"Kafka 事件 topic 前缀；最终 topic 为 {prefix}.capture/.stream/.usage；支持 {ts} 占位符"`
	PublishTimeout    time.Duration `json:"publish-timeout" desc:"单条 Kafka 事件发布超时时间"`
	MaxPublishRetries int           `json:"max-publish-retries" desc:"单条 Kafka 事件临时失败后的最大重试次数"`
	SASL              KafkaSASL     `json:"sasl"                desc:"Kafka SASL 认证配置"`
}

type KafkaSASL struct {
	Username string `json:"username" desc:"Kafka SASL 用户名"`
	Password string `json:"password" desc:"Kafka SASL 密码"`
}

func (k Kafka) BrokerList() []string {
	if strings.TrimSpace(k.Brokers) == "" {
		return nil
	}
	parts := strings.Split(k.Brokers, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		broker := strings.TrimSpace(part)
		if broker != "" {
			out = append(out, broker)
		}
	}
	return out
}

type Plugins struct {
	Usage UsagePlugin `json:"usage" desc:"Responses JSON 字段提取插件配置"`
}

type UsagePlugin struct {
	Enabled  bool          `json:"enabled"  desc:"是否启用 response.completed JSON 字段提取插件"`
	Mode     string        `json:"mode"     desc:"response 字段采集模式：include 或 exclude"`
	Fields   []string      `json:"fields"   desc:"字段列表；include 模式表示保留字段，exclude 模式表示排除字段"`
	Delivery UsageDelivery `json:"delivery" desc:"usage 事件投递配置"`
}

type UsageDelivery struct {
	Enabled       bool          `json:"enabled"        desc:"是否启用 usage 事件 HTTP 投递"`
	Kafka         bool          `json:"kafka"          desc:"是否将 usage 事件投递到 Kafka；要求全局 event.kafka.enabled=true"`
	URL           string        `json:"url"            desc:"usage 事件 HTTP 投递 URL"`
	Token         string        `json:"token"          desc:"usage 事件 HTTP 投递 Bearer token；为空表示不发送认证头"`
	MaxBacklog    int           `json:"max-backlog"    desc:"最大内存积压事件数"`
	FlushInterval time.Duration `json:"flush-interval" desc:"积压事件定时投递间隔"`
	BatchSize     int           `json:"batch-size"     desc:"单次 HTTP 投递最大事件数"`
	Timeout       time.Duration `json:"timeout"        desc:"单次 HTTP 投递超时时间"`
}

func DefaultConfig() Config {
	return Config{
		Server: Server{
			HTTP: ServerHTTP{
				Listen:          ":40174",
				ReadTimeout:     30 * time.Second,
				WriteTimeout:    0,
				IdleTimeout:     120 * time.Second,
				MaxAPIBodyBytes: 1 << 20,
				TLS: ServerHTTPTLS{
					Enabled:        false,
					CertFile:       "${APP_DATA:-.local/data}/ssl/fullchain.pem",
					KeyFile:        "${APP_DATA:-.local/data}/ssl/privkey.pem",
					AutoReload:     true,
					ReloadInterval: 3 * time.Second,
				},
			},
		},
		Proxy: Proxy{
			PathPrefix: "/proxy",
			Transport: ProxyTransport{
				MaxIdleConns:        4096,
				MaxIdleConnsPerHost: 2048,
				MaxConnsPerHost:     0,
				IdleConnTimeout:     60 * time.Second,
				DisableKeepAlives:   false,
			},
		},
		Event: Event{
			Kafka: Kafka{
				Enabled:           false,
				CaptureAbnormal:   true,
				EnsureTopics:      true,
				Brokers:           "${KAFKA_BROKERS:-localhost:9092}",
				TopicPrefix:       "${KAFKA_TOPIC_PREFIX:-prod.directive-proxy}",
				PublishTimeout:    10 * time.Second,
				MaxPublishRetries: 3,
				SASL: KafkaSASL{
					Username: "${KAFKA_SASL_USERNAME}",
					Password: "${KAFKA_SASL_PASSWORD}",
				},
			},
		},
		Plugins: Plugins{
			Usage: UsagePlugin{
				Enabled: true,
				Mode:    "include",
				Fields: []string{
					"id",
					"model",
					"completed_at",
					"created_at",
					"tool_choice",
					"top_logprobs",
					"top_p",
					"usage",
					"reasoning",
				},
				Delivery: UsageDelivery{
					Enabled:       true,
					Kafka:         true,
					URL:           "${USAGE_DELIVERY_URL:-http://localhost:40177/api/metering/events}",
					Token:         "${USAGE_INGEST_TOKEN}",
					MaxBacklog:    1000,
					FlushInterval: 5 * time.Second,
					BatchSize:     10,
					Timeout:       10 * time.Second,
				},
			},
		},
	}
}
