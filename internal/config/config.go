package config

import (
	"errors"
	"time"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
)

var (
	ErrInvalidHTTP      = errors.New("invalid http config")
	ErrInvalidTransport = errors.New("invalid transport config")
)

type Config struct {
	Server Server `json:"server" desc:"服务运行配置"`
	Proxy  Proxy  `json:"proxy"  desc:"代理配置"`
}

type Server struct {
	Debug bool       `json:"debug" desc:"启用调试日志和诊断信息"`
	HTTP  ServerHTTP `json:"http"  desc:"HTTP 服务配置"`
}

type ServerHTTP struct {
	Listen          string           `json:"listen"             desc:"HTTP 服务监听地址"`
	TLS             tlsreload.Config `json:"tls"                desc:"HTTPS TLS 配置"`
	ReadTimeout     time.Duration    `json:"read-timeout"       desc:"HTTP 读取超时时间"`
	WriteTimeout    time.Duration    `json:"write-timeout"      desc:"HTTP 写入超时时间；代理流式响应建议保持 0"`
	IdleTimeout     time.Duration    `json:"idle-timeout"       desc:"HTTP 空闲连接超时时间"`
	MaxAPIBodyBytes int64            `json:"max-api-body-bytes" desc:"Control API 最大请求体字节数，0 表示不限制"`
}

type Proxy struct {
	Transport ProxyTransport `json:"transport" desc:"上游连接池与连接复用配置"`
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
				Listen:          ":23198",
				ReadTimeout:     30 * time.Second,
				WriteTimeout:    0,
				IdleTimeout:     120 * time.Second,
				MaxAPIBodyBytes: 1 << 20,
				TLS: tlsreload.Config{
					Enabled:  false,
					CertFile: "${APP_DATA:-.local/data}/ssl/fullchain.pem",
					KeyFile:  "${APP_DATA:-.local/data}/ssl/privkey.pem",
				},
			},
		},
		Proxy: Proxy{
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
