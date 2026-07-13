package config

import (
	"errors"
	"time"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260711-go-pkg-oidcauth/pkg/oidcauth/dexgithub"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
)

var (
	ErrInvalidHTTP      = errors.New("invalid http config")
	ErrInvalidAuth      = errors.New("invalid auth config")
	ErrInvalidTransport = errors.New("invalid transport config")
	ErrInvalidDirective = errors.New("invalid directive config")
	ErrInvalidAccess    = errors.New("invalid source access config")
)

type Config struct {
	Server Server `json:"server" desc:"服务运行配置"`
	Proxy  Proxy  `json:"proxy"  desc:"代理配置"`
}

type Server struct {
	HTTP ServerHTTP `json:"http" desc:"HTTP 服务配置"`
}

type ServerHTTP struct {
	Listen          string           `json:"listen"             desc:"HTTP 服务监听地址"`
	TLS             tlsreload.Config `json:"tls"                desc:"HTTPS TLS 配置"`
	OIDCAuth        dexgithub.Config `json:"oidc-auth"          desc:"Control API OIDC 认证配置"`
	ReadTimeout     time.Duration    `json:"read-timeout"       desc:"HTTP 读取超时时间"`
	WriteTimeout    time.Duration    `json:"write-timeout"      desc:"HTTP 写入超时时间；代理流式响应建议保持 0"`
	IdleTimeout     time.Duration    `json:"idle-timeout"       desc:"HTTP 空闲连接超时时间"`
	MaxAPIBodyBytes int64            `json:"max-api-body-bytes" desc:"Control API 最大请求体字节数，0 表示不限制"`
	MaxHeaderBytes  int              `json:"max-header-bytes"   desc:"HTTP 请求头最大字节数"`
}

type Proxy struct {
	Transport ProxyTransport `json:"transport" desc:"上游连接池与连接复用配置"`
	Directive ProxyDirective `json:"directive" desc:"指令来源配置"`
}

type ProxyDirective struct {
	MaxTokenBytes  int64               `json:"max-token-bytes"  desc:"directive token 最大字节数"`
	MaxInlineBytes int64               `json:"max-inline-bytes" desc:"inline directive JSON 最大字节数"`
	SourceAccess   sourceaccess.Config `json:"source-access"   desc:"Directive 入口来源白名单"`
	Remote         RemoteDirective     `json:"remote"           desc:"远程指令解析资源限制"`
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
				OIDCAuth: dexgithub.Config{
					Issuer:       "https://2008.s.lwmacct.com:20088",
					ClientID:     "dproxy",
					ExternalURLs: []string{"http://localhost:23199"},
					AllowedUsers: []string{"lwmacct"},
					SessionTTL:   24 * time.Hour,
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
			Directive: ProxyDirective{
				MaxTokenBytes:  64 << 10,
				MaxInlineBytes: 48 << 10,
				SourceAccess: sourceaccess.Config{
					AllowedSources: []string{"127.0.0.1", "::1"},
					DNS: sourceaccess.DNSConfig{
						LookupTimeout: 2 * time.Second,
						SuccessTTL:    time.Minute,
						FailureTTL:    10 * time.Second,
						StaleTTL:      10 * time.Minute,
					},
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
	}
}
