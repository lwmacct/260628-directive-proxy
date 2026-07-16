package config

import (
	"errors"
	"time"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme/adapters/dexgithub"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme/adapters/statictoken"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"
)

var (
	ErrInvalidHTTP      = errors.New("invalid http config")
	ErrInvalidAuth      = errors.New("invalid auth config")
	ErrInvalidTransport = errors.New("invalid transport config")
	ErrInvalidRecovery  = errors.New("invalid recovery config")
	ErrInvalidBodyStore = errors.New("invalid body store config")
	ErrInvalidFluent    = errors.New("invalid fluent config")
	ErrInvalidDirective = errors.New("invalid directive config")
	ErrInvalidAccess    = errors.New("invalid source access config")
)

type Config struct {
	Server Server `json:"server" desc:"服务运行配置"`
}

type Server struct {
	HTTP   ServerHTTP    `json:"http"   desc:"HTTP 服务配置"`
	Proxy  Proxy         `json:"proxy"  desc:"代理配置"`
	Fluent fluent.Config `json:"fluent" desc:"Fluent Forward 事件输出配置；关闭时不创建 Sink、Queue 或连接"`
}

type ServerHTTP struct {
	Listen          string           `json:"listen"            desc:"HTTP 服务监听地址"`
	TLS             tlsreload.Config `json:"tls"               desc:"HTTPS TLS 配置"`
	AuthMe          AuthMe           `json:"authme"            desc:"Authme 指令工作台与工具 API 认证配置"`
	ReadTimeout     time.Duration    `json:"read-timeout"       desc:"HTTP 读取超时时间"`
	WriteTimeout    time.Duration    `json:"write-timeout"      desc:"HTTP 写入超时时间；代理流式响应建议保持 0"`
	IdleTimeout     time.Duration    `json:"idle-timeout"       desc:"HTTP 空闲连接超时时间"`
	MaxAPIBodyBytes int64            `json:"max-api-body-bytes" desc:"指令工具 API 最大请求体字节数，0 表示不限制"`
	MaxHeaderBytes  int              `json:"max-header-bytes"   desc:"HTTP 请求头最大字节数"`
}

type AuthMe struct {
	PathPrefix         string               `json:"path-prefix"      desc:"认证 HTTP 路由前缀"`
	Origins            []string             `json:"origins"          desc:"允许浏览器访问应用的可信 origin"`
	Session            authme.SessionConfig `json:"session"          desc:"统一浏览器 Session 配置"`
	StaticToken        statictoken.Config   `json:"statictoken" desc:"Static token 认证配置"`
	DexGitHub          dexgithub.Config     `json:"dexgithub" desc:"Dex GitHub OIDC 认证配置"`
	AllowedGitHubUsers []string             `json:"allowed-github-users" desc:"允许访问应用的 GitHub 用户名"`
}

type Proxy struct {
	Transport ProxyTransport `json:"transport" desc:"上游连接池与连接复用配置"`
	Recovery  ProxyRecovery  `json:"recovery"  desc:"Directive Recovery Controller 全局资源上限"`
	BodyStore ProxyBodyStore `json:"body-store" desc:"流式可重放请求正文的内存与磁盘配置"`
	Directive ProxyDirective `json:"directive" desc:"指令来源配置"`
}

type ProxyRecovery struct {
	MaxAttemptsLimit         int           `json:"max-attempts-limit"         desc:"Directive 可声明的最大 Attempt 数上限"`
	MaxElapsedLimit          time.Duration `json:"max-elapsed-limit"          desc:"Directive 可声明的最大 Recovery 总时长"`
	MaxCallbackTimeout       time.Duration `json:"max-callback-timeout"       desc:"单次 Recovery Controller 回调超时上限"`
	MaxCapturedBodyBytes     int64         `json:"max-captured-body-bytes"    desc:"异常上游响应允许捕获的最大正文大小"`
	MaxCallbackResponseBytes int64         `json:"max-callback-response-bytes" desc:"Recovery Controller 决策响应最大大小"`
}

type ProxyBodyStore struct {
	MemoryMaxBytes     int64         `json:"memory-max-bytes" desc:"所有活动 Replay Store 使用的最大内存字节数"`
	MemoryPerBodyBytes int64         `json:"memory-per-body-bytes" desc:"单个请求 spill 到磁盘前保留在内存的最大字节数"`
	DiskMaxBytes       int64         `json:"disk-max-bytes" desc:"所有活动 Replay Store 使用的最大临时磁盘字节数"`
	MaxBodyBytes       int64         `json:"max-body-bytes" desc:"单个请求正文允许的最大实际字节数"`
	ChunkBytes         int           `json:"chunk-bytes" desc:"正文摄取和内存分段的 chunk 字节数"`
	TempDir            string        `json:"temp-dir" desc:"匿名 spill 临时文件目录"`
	ReadTimeout        time.Duration `json:"body-read-timeout" desc:"读取请求正文的最长时间"`
}

type ProxyDirective struct {
	MaxTokenBytes  int64                 `json:"max-token-bytes"  desc:"directive token 最大字节数"`
	MaxInlineBytes int64                 `json:"max-inline-bytes" desc:"inline directive JSON 最大字节数"`
	SourceAccess   DirectiveSourceAccess `json:"source-access"   desc:"Directive 入口来源白名单"`
	Remote         RemoteDirective       `json:"remote"           desc:"远程指令解析资源限制"`
}

type DirectiveSourceAccess struct {
	sourceaccess.Config `cfgm:",inline"`
	TrustedProxies      []string `json:"trusted-proxies" desc:"可信反向代理 IP/CIDR 列表，仅这些来源可提供真实客户端 IP 头"`
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
	return Config{
		Server: Server{
			HTTP: ServerHTTP{
				Listen: ":23198",
				AuthMe: AuthMe{
					Origins: []string{"http://localhost:23199"},
					Session: authme.SessionConfig{
						Keys: []authme.SessionKey{{ID: "default", Secret: "${AUTHME_SESSION_KEY}"}},
					},
					StaticToken: func() statictoken.Config {
						config := statictoken.DefaultConfig()
						config.Enabled = true
						config.Credentials = []statictoken.Credential{{ID: "admin", Name: "Administrator", Token: "${AUTHME_ACCESS_TOKEN}"}}
						return config
					}(),
					DexGitHub: func() dexgithub.Config {
						config := dexgithub.DefaultConfig()
						config.Issuer = "https://2008.s.lwmacct.com:20088"
						config.ClientID = "dproxy"
						return config
					}(),
					AllowedGitHubUsers: []string{"lwmacct"},
				},
				ReadTimeout:     30 * time.Second,
				WriteTimeout:    0,
				IdleTimeout:     120 * time.Second,
				MaxAPIBodyBytes: 1 << 20,
				MaxHeaderBytes:  128 << 10,
				TLS: func() tlsreload.Config {
					config := tlsreload.DefaultConfig()
					config.DefaultCertificate = "default"
					config.Certificates = []tlsreload.CertificateSource{
						{
							ID:          "default",
							Certificate: "${APP_DATA:-.local/data}/ssl/fullchain.pem",
							PrivateKey:  "${APP_DATA:-.local/data}/ssl/privkey.pem",
						},
					}
					return config
				}(),
			},
			Proxy: Proxy{
				Recovery: ProxyRecovery{
					MaxAttemptsLimit:         10,
					MaxElapsedLimit:          2 * time.Minute,
					MaxCallbackTimeout:       5 * time.Second,
					MaxCapturedBodyBytes:     1 << 20,
					MaxCallbackResponseBytes: 16 << 10,
				},
				BodyStore: ProxyBodyStore{
					MemoryMaxBytes:     512 << 20,
					MemoryPerBodyBytes: 1 << 20,
					DiskMaxBytes:       8 << 30,
					MaxBodyBytes:       32 << 20,
					ChunkBytes:         64 << 10,
					TempDir:            "${APP_DATA:-.local/data}/tmp/body-store",
					ReadTimeout:        30 * time.Second,
				},
				Directive: ProxyDirective{
					MaxTokenBytes:  64 << 10,
					MaxInlineBytes: 48 << 10,
					SourceAccess: func() DirectiveSourceAccess {
						access := sourceaccess.DefaultConfig()
						access.Rules = []sourceaccess.Rule{
							{Value: "127.0.0.1"},
							{Value: "::1"},
							{Value: "172.22.0.0/16"},
						}
						access.DNS.StaleTTL = 10 * time.Minute
						return DirectiveSourceAccess{Config: access}
					}(),
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
			Fluent: func() fluent.Config {
				config := fluent.DefaultConfig()
				config.Endpoint = "${FLUENT_ENDPOINT:-unix:///run/fluent/fluent.sock}"
				config.TagPrefix = "dproxy"
				return config
			}(),
		},
	}
}

var Manager = cfgm.New(
	DefaultConfig(),
	cfgm.AppName("app"),
)
