package config

import (
	"errors"
	"slices"
	"time"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/oidc"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/statictoken"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
)

var (
	ErrInvalidHTTP          = errors.New("invalid http config")
	ErrInvalidAuth          = errors.New("invalid auth config")
	ErrInvalidTransport     = errors.New("invalid transport config")
	ErrInvalidRetry         = errors.New("invalid retry config")
	ErrInvalidObservability = errors.New("invalid observability config")
	ErrInvalidDirective     = errors.New("invalid directive config")
	ErrInvalidAccess        = errors.New("invalid source access config")
)

type Config struct {
	Server        Server        `json:"server"        desc:"服务运行配置"`
	Proxy         Proxy         `json:"proxy"         desc:"代理配置"`
	Observability Observability `json:"observability" desc:"可观测插件与输出配置"`
}

type Server struct {
	HTTP ServerHTTP `json:"http" desc:"HTTP 服务配置"`
}

type ServerHTTP struct {
	Listen          string           `json:"listen"            desc:"HTTP 服务监听地址"`
	TLS             tlsreload.Config `json:"tls"               desc:"HTTPS TLS 配置"`
	Auth            Auth             `json:"auth"              desc:"Control API 认证配置"`
	ReadTimeout     time.Duration    `json:"read-timeout"       desc:"HTTP 读取超时时间"`
	WriteTimeout    time.Duration    `json:"write-timeout"      desc:"HTTP 写入超时时间；代理流式响应建议保持 0"`
	IdleTimeout     time.Duration    `json:"idle-timeout"       desc:"HTTP 空闲连接超时时间"`
	MaxAPIBodyBytes int64            `json:"max-api-body-bytes" desc:"Control API 最大请求体字节数，0 表示不限制"`
	MaxHeaderBytes  int              `json:"max-header-bytes"   desc:"HTTP 请求头最大字节数"`
}

type AuthMethod string

const (
	AuthMethodOIDC  AuthMethod = "oidc"
	AuthMethodToken AuthMethod = "token"
)

type Auth struct {
	ExternalURLs []string               `json:"external-urls" desc:"允许浏览器访问应用的可信 origin"`
	Session      httpauth.SessionConfig `json:"session"       desc:"统一浏览器 Session 配置"`
	Methods      []AuthMethod           `json:"methods"       desc:"启用的认证方式，可选 token、oidc"`
	Token        statictoken.Config     `json:"token"         desc:"Token 认证配置"`
	OIDC         OIDCAuth               `json:"oidc"          desc:"OIDC 认证配置"`
}

type OIDCAuth struct {
	Issuer       string        `json:"issuer"        desc:"Dex issuer URL"`
	ClientID     string        `json:"client-id"     desc:"Dex OIDC client ID"`
	ClientSecret string        `json:"client-secret" desc:"Dex confidential client secret；public client 留空"`
	AllowedUsers []string      `json:"allowed-users" desc:"允许访问应用的 GitHub 用户名"`
	SessionTTL   time.Duration `json:"session-ttl"   desc:"OIDC 身份 Session 最长有效时间"`
}

func (c OIDCAuth) MethodConfig() oidc.Config {
	return oidc.Config{ID: "github", Label: "GitHub", Issuer: c.Issuer, ClientID: c.ClientID, ClientSecret: c.ClientSecret, SessionTTL: c.SessionTTL}
}

type Proxy struct {
	Transport ProxyTransport `json:"transport" desc:"上游连接池与连接复用配置"`
	Retry     ProxyRetry     `json:"retry"     desc:"等待上游响应时的外部介入重试配置"`
	Directive ProxyDirective `json:"directive" desc:"指令来源配置"`
}

type ProxyRetry struct {
	Enabled           bool          `json:"enabled"            desc:"是否启用等待响应请求的外部介入重试"`
	RetryableAfter    time.Duration `json:"retryable-after"    desc:"单次上游请求等待多久后允许外部介入重试"`
	MaxAttempts       int           `json:"max-attempts"       desc:"单个逻辑请求允许的最大上游尝试次数，包含首次请求"`
	MaxActiveRequests int           `json:"max-active-requests" desc:"同时等待上游最终响应的最大请求数"`
	TempDir           string        `json:"temp-dir"           desc:"可重放请求正文临时文件目录，留空使用系统临时目录"`
	MaxBodyBytes      int64         `json:"max-body-bytes"     desc:"单个可重放请求正文最大字节数"`
	MaxInflightBytes  int64         `json:"max-inflight-bytes" desc:"所有进行中请求正文临时文件的总字节上限"`
	BufferChunkBytes  int           `json:"buffer-chunk-bytes" desc:"读取并写入可重放请求正文时的分块大小"`
}

type Observability struct {
	InstanceID string                `json:"instance-id" desc:"写入观测记录的代理实例标识，留空使用主机名"`
	Plugins    []ObservationPlugin   `json:"plugins"     desc:"内置观测插件列表"`
	Outputs    []ObservabilityOutput `json:"outputs"     desc:"观测记录输出插件列表"`
}

type ObservationPlugin struct {
	Name     string                `json:"name"      desc:"插件实例名称"`
	Type     string                `json:"type"      desc:"插件类型：builtin.capture 或 builtin.llmusage"`
	Enabled  bool                  `json:"enabled"   desc:"是否启用插件"`
	Capture  *CapturePluginConfig  `json:"capture,omitempty"   desc:"builtin.capture 配置"`
	LLMUsage *LLMUsagePluginConfig `json:"llmusage,omitempty" desc:"builtin.llmusage 配置"`
}

type CapturePluginConfig struct {
	BodyChunkBytes   int      `json:"body-chunk-bytes"    desc:"请求和响应正文单条记录的最大字节数"`
	MaxSSEEventBytes int      `json:"max-sse-event-bytes" desc:"单条 SSE 语义事件解析缓冲上限"`
	RedactHeaders    []string `json:"redact-headers"      desc:"需要脱敏的 HTTP header 名称或 glob"`
	RedactQuery      []string `json:"redact-query"        desc:"需要脱敏的 URL query 参数名称或 glob"`
}

type LLMUsagePluginConfig struct {
	MaxSSEMetadataBytes int `json:"max-sse-metadata-bytes" desc:"单个 SSE event metadata 保留上限，0 使用库默认值"`
	MaxResultBytes      int `json:"max-result-bytes"       desc:"协议识别字段和 raw usage 保留上限，0 使用库默认值"`
	MaxNestingDepth     int `json:"max-nesting-depth"      desc:"JSON 最大嵌套深度，0 使用库默认值"`
}

type ObservabilityOutput struct {
	Name    string        `json:"name"    desc:"输出实例名称"`
	Type    string        `json:"type"    desc:"输出类型：fluent"`
	Enabled bool          `json:"enabled" desc:"是否启用输出"`
	Routes  []string      `json:"routes"  desc:"接收的 record topic 路由"`
	Workers int           `json:"workers" desc:"按 trace ID 分片的输出 worker 数"`
	Queue   OutputQueue   `json:"queue"   desc:"Pipeline 输出队列配置"`
	Fluent  *FluentOutput `json:"fluent,omitempty"  desc:"Fluent Forward 输出配置"`
}

type OutputQueue struct {
	Capacity int   `json:"capacity"  desc:"每个 worker 的记录队列容量"`
	MaxBytes int64 `json:"max-bytes" desc:"输出所有 worker 合计的排队字节上限"`
}

type FluentOutput struct {
	Endpoint              string        `json:"endpoint"                 desc:"Fluent Forward endpoint，支持 tcp、tls、unix、ws 和 wss"`
	Connections           int           `json:"connections"              desc:"按 trace ID 分片的 Fluent 客户端数"`
	ClientQueueCapacity   int           `json:"client-queue-capacity"    desc:"每个 Fluent client 的内部发送队列容量"`
	ConnectTimeout        time.Duration `json:"connect-timeout"          desc:"Fluent 建连超时"`
	HandshakeTimeout      time.Duration `json:"handshake-timeout"        desc:"Fluent Forward 握手超时"`
	WriteTimeout          time.Duration `json:"write-timeout"            desc:"Fluent 单条记录写入超时"`
	ACKTimeout            time.Duration `json:"ack-timeout"              desc:"Fluent ACK 读取超时"`
	RetryMaxAttempts      int           `json:"retry-max-attempts"       desc:"Fluent 单条记录最大投递尝试次数"`
	RetryMinBackoff       time.Duration `json:"retry-min-backoff"        desc:"Fluent 重试最短退避时间"`
	RetryMaxBackoff       time.Duration `json:"retry-max-backoff"        desc:"Fluent 重试最长退避时间"`
	TagPrefix             string        `json:"tag-prefix"               desc:"Fluent tag 前缀"`
	Delivery              string        `json:"delivery"                 desc:"Fluent 投递模式：unconfirmed 或 at-least-once"`
	TLSInsecureSkipVerify bool          `json:"tls-insecure-skip-verify" desc:"是否跳过 Fluent TLS 证书验证，仅用于开发"`
}

const (
	ObservationPluginCapture  = "builtin.capture"
	ObservationPluginLLMUsage = "builtin.llmusage"
	ObservabilityOutputFluent = "fluent"
)

const (
	FluentDeliveryUnconfirmed = "unconfirmed"
	FluentDeliveryAtLeastOnce = "at-least-once"
)

type ProxyDirective struct {
	MaxTokenBytes  int64                 `json:"max-token-bytes"  desc:"directive token 最大字节数"`
	MaxInlineBytes int64                 `json:"max-inline-bytes" desc:"inline directive JSON 最大字节数"`
	SourceAccess   DirectiveSourceAccess `json:"source-access"   desc:"Directive 入口来源白名单"`
	Remote         RemoteDirective       `json:"remote"           desc:"远程指令解析资源限制"`
}

type DirectiveSourceAccess struct {
	Enabled        bool                   `json:"enabled"          desc:"是否启用 Directive 入口来源白名单"`
	AllowedSources []string               `json:"allowed-sources"  desc:"允许访问的来源 IP、CIDR 或域名列表"`
	TrustedProxies []string               `json:"trusted-proxies" desc:"可信反向代理 IP/CIDR 列表，仅这些来源可提供真实客户端 IP 头"`
	DNS            sourceaccess.DNSConfig `json:"dns"             desc:"域名来源规则的 DNS 缓存配置"`
}

type RemoteDirective struct {
	Timeout          time.Duration        `json:"timeout"            desc:"单次远程指令解析总超时"`
	MaxResponseBytes int64                `json:"max-response-bytes" desc:"远程 directive JSON 最大字节数"`
	HTTP             HTTPRemoteDirective  `json:"http"               desc:"HTTP resolver 资源限制"`
	Redis            RedisRemoteDirective `json:"redis"              desc:"Redis adapter 资源限制"`
}

type HTTPRemoteDirective struct {
	MaxRequestBytes int64 `json:"max-request-bytes" desc:"HTTP resolver 请求元数据最大字节数"`
}

type RedisRemoteDirective struct {
	ClientCacheCapacity int           `json:"client-cache-capacity" desc:"动态 Redis client 缓存容量"`
	ClientIdleTimeout   time.Duration `json:"client-idle-timeout"   desc:"动态 Redis client 空闲回收时间"`
	PoolSize            int           `json:"pool-size"             desc:"每个动态 Redis client 的连接池容量"`
}

type ProxyTransport struct {
	MaxIdleConns        int           `json:"max-idle-conns"         desc:"全局空闲连接池容量；只影响连接复用，不限制活跃并发"`
	MaxIdleConnsPerHost int           `json:"max-idle-conns-per-host" desc:"单 upstream 空闲连接池容量；只影响连接复用，不限制活跃并发"`
	MaxConnsPerHost     int           `json:"max-conns-per-host"      desc:"单 upstream 活跃连接上限；0 表示不限制"`
	IdleConnTimeout     time.Duration `json:"idle-conn-timeout"       desc:"空闲连接在连接池中的保留时间"`
	DisableKeepAlives   bool          `json:"disable-keep-alives"    desc:"是否禁用上游 keep-alive"`
}

func DefaultConfig() Config {
	sourceDNS := sourceaccess.DefaultDNSConfig()
	sourceDNS.StaleTTL = 10 * time.Minute
	return Config{
		Server: Server{
			HTTP: ServerHTTP{
				Listen: ":23198",
				Auth: Auth{
					Methods:      []AuthMethod{AuthMethodToken},
					ExternalURLs: []string{"http://localhost:23199"},
					Session: httpauth.SessionConfig{
						Keys: []httpauth.SessionKey{{ID: "default", Secret: "${AUTH_SESSION_KEY}"}},
						TTL:  24 * time.Hour,
					},
					Token: statictoken.Config{
						Credentials: map[string]statictoken.Credential{
							"admin": {Name: "Administrator", TokenSHA256: "${AUTH_TOKEN_SHA256}"},
						},
					},
					OIDC: OIDCAuth{
						Issuer:       "https://2008.s.lwmacct.com:20088",
						ClientID:     "dproxy",
						AllowedUsers: []string{"lwmacct"},
						SessionTTL:   24 * time.Hour,
					},
				},
				ReadTimeout:     30 * time.Second,
				WriteTimeout:    0,
				IdleTimeout:     120 * time.Second,
				MaxAPIBodyBytes: 1 << 20,
				MaxHeaderBytes:  128 << 10,
				TLS: tlsreload.Config{
					Enabled:  false,
					CertFile: "${APP_DATA:-.local/data}/ssl/fullchain.pem",
					KeyFile:  "${APP_DATA:-.local/data}/ssl/privkey.pem",
				},
			},
		},
		Proxy: Proxy{
			Retry: ProxyRetry{
				Enabled:           true,
				RetryableAfter:    10 * time.Second,
				MaxAttempts:       3,
				MaxActiveRequests: 4096,
				MaxBodyBytes:      32 << 20,
				MaxInflightBytes:  1 << 30,
				BufferChunkBytes:  32 << 10,
			},
			Directive: ProxyDirective{
				MaxTokenBytes:  64 << 10,
				MaxInlineBytes: 48 << 10,
				SourceAccess: DirectiveSourceAccess{
					Enabled:        false,
					AllowedSources: []string{"127.0.0.1", "::1", "172.22.0.0/16"},
					DNS:            sourceDNS,
				},
				Remote: RemoteDirective{
					Timeout:          time.Second,
					MaxResponseBytes: 256 << 10,
					HTTP: HTTPRemoteDirective{
						MaxRequestBytes: 128 << 10,
					},
					Redis: RedisRemoteDirective{
						ClientCacheCapacity: 64,
						ClientIdleTimeout:   10 * time.Minute,
						PoolSize:            4,
					},
				},
			},
			Transport: ProxyTransport{
				MaxIdleConns:        4096,
				MaxIdleConnsPerHost: 2048,
				MaxConnsPerHost:     0,
				IdleConnTimeout:     60 * time.Second,
				DisableKeepAlives:   false,
			},
		},
		Observability: Observability{
			Plugins: []ObservationPlugin{
				{
					Name: "capture", Type: ObservationPluginCapture, Enabled: false,
					Capture: &CapturePluginConfig{
						BodyChunkBytes: 32 << 10, MaxSSEEventBytes: 1 << 20,
						RedactHeaders: []string{"authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key"},
						RedactQuery:   []string{"access_token", "api_key", "apikey", "key", "token"},
					},
				},
				{Name: "llmusage", Type: ObservationPluginLLMUsage, Enabled: false, LLMUsage: &LLMUsagePluginConfig{}},
			},
			Outputs: []ObservabilityOutput{
				{
					Name: "fluent-primary", Type: ObservabilityOutputFluent, Enabled: false,
					Routes: []string{"capture.**", "llm.usage.**"}, Workers: 4,
					Queue: OutputQueue{Capacity: 8192, MaxBytes: 256 << 20},
					Fluent: &FluentOutput{
						Endpoint: "unix:///run/fluent/fluent.sock", Connections: 4, ClientQueueCapacity: 1024,
						ConnectTimeout: 500 * time.Millisecond, HandshakeTimeout: 500 * time.Millisecond,
						WriteTimeout: 500 * time.Millisecond, ACKTimeout: 500 * time.Millisecond,
						RetryMaxAttempts: 1, RetryMinBackoff: 100 * time.Millisecond, RetryMaxBackoff: 500 * time.Millisecond,
						TagPrefix: "dproxy", Delivery: FluentDeliveryAtLeastOnce,
					},
				},
			},
		},
	}
}

func (c Auth) HasMethod(method AuthMethod) bool {
	return slices.Contains(c.Methods, method)
}
