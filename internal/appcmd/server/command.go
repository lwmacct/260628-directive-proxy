package server

import (
	"context"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
	"github.com/lwmacct/251207-go-pkg-version/pkg/version"
	"github.com/urfave/cli/v3"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
)

var (
	defaults = config.DefaultConfig()
	flagHelp = cfgm.Schema(defaults).Command("server")
)

var Command = &cli.Command{
	Name:            "server",
	Usage:           "start directive proxy server",
	Action:          action,
	Commands:        []*cli.Command{version.Command},
	HideHelpCommand: true,
	Flags:           commandFlags(),
}

func action(ctx context.Context, cmd *cli.Command) error {
	cfg := cfgm.MustLoad(ctx, config.DefaultConfig(), cfgm.Command(cmd))
	return NewApp(cfg).Run(ctx)
}

func commandFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "http.listen", Usage: flagHelp.MustUsage("http.listen"), Value: defaults.Server.HTTP.Listen},
		&cli.StringFlag{Name: "http.oidc-auth.issuer", Usage: flagHelp.MustUsage("http.oidc-auth.issuer"), Value: defaults.Server.HTTP.OIDCAuth.Issuer},
		&cli.StringFlag{Name: "http.oidc-auth.client-id", Usage: flagHelp.MustUsage("http.oidc-auth.client-id"), Value: defaults.Server.HTTP.OIDCAuth.ClientID},
		&cli.StringFlag{Name: "http.oidc-auth.client-secret", Usage: flagHelp.MustUsage("http.oidc-auth.client-secret"), Value: defaults.Server.HTTP.OIDCAuth.ClientSecret},
		&cli.StringSliceFlag{Name: "http.oidc-auth.external-urls", Usage: flagHelp.MustUsage("http.oidc-auth.external-urls"), Value: defaults.Server.HTTP.OIDCAuth.ExternalURLs},
		&cli.StringSliceFlag{Name: "http.oidc-auth.allowed-users", Usage: flagHelp.MustUsage("http.oidc-auth.allowed-users"), Value: defaults.Server.HTTP.OIDCAuth.AllowedUsers},
		&cli.DurationFlag{Name: "http.oidc-auth.session-ttl", Usage: flagHelp.MustUsage("http.oidc-auth.session-ttl"), Value: defaults.Server.HTTP.OIDCAuth.SessionTTL},
		&cli.BoolFlag{Name: "http.tls.enabled", Usage: flagHelp.MustUsage("http.tls.enabled"), Value: defaults.Server.HTTP.TLS.Enabled},
		&cli.StringFlag{Name: "http.tls.cert-file", Usage: flagHelp.MustUsage("http.tls.cert-file"), Value: defaults.Server.HTTP.TLS.CertFile},
		&cli.StringFlag{Name: "http.tls.key-file", Usage: flagHelp.MustUsage("http.tls.key-file"), Value: defaults.Server.HTTP.TLS.KeyFile},
		&cli.DurationFlag{Name: "http.tls.poll-interval", Usage: flagHelp.MustUsage("http.tls.poll-interval"), Value: defaults.Server.HTTP.TLS.PollInterval},
		&cli.DurationFlag{Name: "http.read-timeout", Usage: flagHelp.MustUsage("http.read-timeout"), Value: defaults.Server.HTTP.ReadTimeout},
		&cli.DurationFlag{Name: "http.write-timeout", Usage: flagHelp.MustUsage("http.write-timeout"), Value: defaults.Server.HTTP.WriteTimeout},
		&cli.DurationFlag{Name: "http.idle-timeout", Usage: flagHelp.MustUsage("http.idle-timeout"), Value: defaults.Server.HTTP.IdleTimeout},
		&cli.Int64Flag{Name: "http.max-api-body-bytes", Usage: flagHelp.MustUsage("http.max-api-body-bytes"), Value: defaults.Server.HTTP.MaxAPIBodyBytes},
		&cli.IntFlag{Name: "http.max-header-bytes", Usage: flagHelp.MustUsage("http.max-header-bytes"), Value: defaults.Server.HTTP.MaxHeaderBytes},
		&cli.IntFlag{Name: "proxy.transport.max-idle-conns", Usage: flagHelp.MustUsage("proxy.transport.max-idle-conns"), Value: defaults.Proxy.Transport.MaxIdleConns},
		&cli.IntFlag{Name: "proxy.transport.max-idle-conns-per-host", Usage: flagHelp.MustUsage("proxy.transport.max-idle-conns-per-host"), Value: defaults.Proxy.Transport.MaxIdleConnsPerHost},
		&cli.IntFlag{Name: "proxy.transport.max-conns-per-host", Usage: flagHelp.MustUsage("proxy.transport.max-conns-per-host"), Value: defaults.Proxy.Transport.MaxConnsPerHost},
		&cli.DurationFlag{Name: "proxy.transport.idle-conn-timeout", Usage: flagHelp.MustUsage("proxy.transport.idle-conn-timeout"), Value: defaults.Proxy.Transport.IdleConnTimeout},
		&cli.BoolFlag{Name: "proxy.transport.disable-keep-alives", Usage: flagHelp.MustUsage("proxy.transport.disable-keep-alives"), Value: defaults.Proxy.Transport.DisableKeepAlives},
		&cli.Int64Flag{Name: "proxy.directive.max-token-bytes", Usage: flagHelp.MustUsage("proxy.directive.max-token-bytes"), Value: defaults.Proxy.Directive.MaxTokenBytes},
		&cli.Int64Flag{Name: "proxy.directive.max-inline-bytes", Usage: flagHelp.MustUsage("proxy.directive.max-inline-bytes"), Value: defaults.Proxy.Directive.MaxInlineBytes},
		&cli.DurationFlag{Name: "proxy.directive.remote.timeout", Usage: flagHelp.MustUsage("proxy.directive.remote.timeout"), Value: defaults.Proxy.Directive.Remote.Timeout},
		&cli.Int64Flag{Name: "proxy.directive.remote.http.max-request-bytes", Usage: flagHelp.MustUsage("proxy.directive.remote.http.max-request-bytes"), Value: defaults.Proxy.Directive.Remote.HTTP.MaxRequestBytes},
		&cli.Int64Flag{Name: "proxy.directive.remote.max-response-bytes", Usage: flagHelp.MustUsage("proxy.directive.remote.max-response-bytes"), Value: defaults.Proxy.Directive.Remote.MaxResponseBytes},
		&cli.IntFlag{Name: "proxy.directive.remote.redis.client-cache-capacity", Usage: flagHelp.MustUsage("proxy.directive.remote.redis.client-cache-capacity"), Value: defaults.Proxy.Directive.Remote.Redis.ClientCacheCapacity},
		&cli.DurationFlag{Name: "proxy.directive.remote.redis.client-idle-timeout", Usage: flagHelp.MustUsage("proxy.directive.remote.redis.client-idle-timeout"), Value: defaults.Proxy.Directive.Remote.Redis.ClientIdleTimeout},
		&cli.IntFlag{Name: "proxy.directive.remote.redis.pool-size", Usage: flagHelp.MustUsage("proxy.directive.remote.redis.pool-size"), Value: defaults.Proxy.Directive.Remote.Redis.PoolSize},
	}
}
