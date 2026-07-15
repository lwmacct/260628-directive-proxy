package config

import (
	"errors"
	"slices"
	"time"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/oidc"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/statictoken"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
)

var (
	ErrInvalidHTTP      = errors.New("invalid http config")
	ErrInvalidAuth      = errors.New("invalid auth config")
	ErrInvalidTransport = errors.New("invalid transport config")
	ErrInvalidRetry     = errors.New("invalid retry config")
	ErrInvalidFluent    = errors.New("invalid fluent config")
	ErrInvalidDirective = errors.New("invalid directive config")
	ErrInvalidAccess    = errors.New("invalid source access config")
)

type Config struct {
	Server Server `json:"server" desc:"服务运行配置"`
}

type Server struct {
	HTTP   ServerHTTP   `json:"http"   desc:"HTTP 服务配置"`
	Proxy  Proxy        `json:"proxy"  desc:"代理配置"`
	Fluent FluentOutput `json:"fluent" desc:"Fluent Forward 观测输出配置；关闭时禁用所有观测插件"`
}

type ServerHTTP struct {
	Listen          string        `json:"listen"            desc:"HTTP 服务监听地址"`
	TLS             TLSConfig     `json:"tls"               desc:"HTTPS TLS 配置"`
	Auth            Auth          `json:"auth"              desc:"Admin API 认证配置"`
	ReadTimeout     time.Duration `json:"read-timeout"       desc:"HTTP 读取超时时间"`
	WriteTimeout    time.Duration `json:"write-timeout"      desc:"HTTP 写入超时时间；代理流式响应建议保持 0"`
	IdleTimeout     time.Duration `json:"idle-timeout"       desc:"HTTP 空闲连接超时时间"`
	MaxAPIBodyBytes int64         `json:"max-api-body-bytes" desc:"Admin API 最大请求体字节数，0 表示不限制"`
	MaxHeaderBytes  int           `json:"max-header-bytes"   desc:"HTTP 请求头最大字节数"`
}

type TLSConfig struct {
	tlsreload.Config `cfgm:",inline"`
	Enabled          bool `json:"enabled" desc:"是否启用 HTTPS TLS"`
}

func (c TLSConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	return c.Config.Validate()
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
	Transport  ProxyTransport  `json:"transport" desc:"上游连接池与连接复用配置"`
	Retry      ProxyRetry      `json:"retry"     desc:"等待上游响应时的外部介入重试配置"`
	BodyMemory ProxyBodyMemory `json:"body-memory" desc:"可重放请求正文的内存与等待队列配置"`
	Directive  ProxyDirective  `json:"directive" desc:"指令来源配置"`
}

type ProxyRetry struct {
	MaxAttempts      int           `json:"max-attempts"       desc:"单个逻辑请求允许的最大上游尝试次数，包含首次请求"`
	CommandRetention time.Duration `json:"command-retention"  desc:"已接受重试命令终态的保留时间"`
}

type ProxyBodyMemory struct {
	MaxActiveBytes int64         `json:"max-active-bytes" desc:"所有活动可重放请求正文占用的最大逻辑字节数"`
	MaxBodyBytes   int64         `json:"max-body-bytes"   desc:"单个可重放请求正文最大字节数"`
	QueueMax       int           `json:"queue-max-requests" desc:"等待正文内存的最大请求数"`
	QueueWait      time.Duration `json:"queue-max-wait" desc:"请求等待正文内存的最长时间"`
	ReadTimeout    time.Duration `json:"body-read-timeout" desc:"获得内存后读取完整请求正文的最长时间"`
}

type SinkQueue struct {
	MaxRecords int   `json:"max-records" desc:"所有 Fluent 连接合计的排队记录上限"`
	MaxBytes   int64 `json:"max-bytes"   desc:"所有 Fluent 连接合计的排队字节上限"`
}

type FluentOutput struct {
	Enabled               bool          `json:"enabled"                  desc:"是否启用 Fluent；关闭时禁用全部观测插件且不影响代理请求"`
	Endpoint              string        `json:"endpoint"                 desc:"Fluent Forward endpoint，支持 tcp、tls、unix、ws 和 wss"`
	Connections           int           `json:"connections"              desc:"按 trace ID 分片的 Fluent 连接与发送并行度"`
	Queue                 SinkQueue     `json:"queue"                    desc:"Fluent 异步输出队列配置"`
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
				TLS: TLSConfig{
					Enabled: false,
					Config: tlsreload.Config{
						DefaultCertificate: "default",
						Certificates: []tlsreload.CertificateSource{
							{
								ID:          "default",
								Certificate: "${APP_DATA:-.local/data}/ssl/fullchain.pem",
								PrivateKey:  "${APP_DATA:-.local/data}/ssl/privkey.pem",
							},
						},
					},
				},
			},
			Proxy: Proxy{
				Retry: ProxyRetry{
					MaxAttempts:      3,
					CommandRetention: time.Minute,
				},
				BodyMemory: ProxyBodyMemory{
					MaxActiveBytes: 2 << 30,
					MaxBodyBytes:   32 << 20,
					QueueMax:       512,
					QueueWait:      15 * time.Second,
					ReadTimeout:    30 * time.Second,
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
			Fluent: FluentOutput{
				Enabled: false, Endpoint: "${FLUENT_ENDPOINT:-unix:///run/fluent/fluent.sock}", Connections: 4,
				Queue:          SinkQueue{MaxRecords: 8192, MaxBytes: 256 << 20},
				ConnectTimeout: 500 * time.Millisecond, HandshakeTimeout: 500 * time.Millisecond,
				WriteTimeout: 500 * time.Millisecond, ACKTimeout: 500 * time.Millisecond,
				RetryMaxAttempts: 1, RetryMinBackoff: 100 * time.Millisecond, RetryMaxBackoff: 500 * time.Millisecond,
				TagPrefix: "dproxy", Delivery: FluentDeliveryAtLeastOnce,
			},
		},
	}
}

func (c Auth) HasMethod(method AuthMethod) bool {
	return slices.Contains(c.Methods, method)
}

var Manager = cfgm.New(
	DefaultConfig(),
	cfgm.AppName("app"),
	cfgm.HideCLI(
		"server.http.auth.session.keys",
		"server.http.auth.token",
	),
)
