package server

import (
	"context"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
	"github.com/lwmacct/251207-go-pkg-version/pkg/version"
	"github.com/urfave/cli/v3"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
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
		&cli.StringSliceFlag{Name: "http.auth.external-urls", Usage: flagHelp.MustUsage("http.auth.external-urls"), Value: defaults.Server.HTTP.Auth.ExternalURLs},
		&cli.DurationFlag{Name: "http.auth.session.ttl", Usage: flagHelp.MustUsage("http.auth.session.ttl"), Value: defaults.Server.HTTP.Auth.Session.TTL},
		&cli.StringSliceFlag{Name: "http.auth.methods", Usage: flagHelp.MustUsage("http.auth.methods"), Value: authMethodFlagValues(defaults.Server.HTTP.Auth.Methods)},
		&cli.StringFlag{Name: "http.auth.oidc.issuer", Usage: flagHelp.MustUsage("http.auth.oidc.issuer"), Value: defaults.Server.HTTP.Auth.OIDC.Issuer},
		&cli.StringFlag{Name: "http.auth.oidc.client-id", Usage: flagHelp.MustUsage("http.auth.oidc.client-id"), Value: defaults.Server.HTTP.Auth.OIDC.ClientID},
		&cli.StringFlag{Name: "http.auth.oidc.client-secret", Usage: flagHelp.MustUsage("http.auth.oidc.client-secret"), Value: defaults.Server.HTTP.Auth.OIDC.ClientSecret},
		&cli.StringSliceFlag{Name: "http.auth.oidc.allowed-users", Usage: flagHelp.MustUsage("http.auth.oidc.allowed-users"), Value: defaults.Server.HTTP.Auth.OIDC.AllowedUsers},
		&cli.DurationFlag{Name: "http.auth.oidc.session-ttl", Usage: flagHelp.MustUsage("http.auth.oidc.session-ttl"), Value: defaults.Server.HTTP.Auth.OIDC.SessionTTL},
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
		&cli.BoolFlag{Name: "proxy.retry.enabled", Usage: flagHelp.MustUsage("proxy.retry.enabled"), Value: defaults.Proxy.Retry.Enabled},
		&cli.DurationFlag{Name: "proxy.retry.retryable-after", Usage: flagHelp.MustUsage("proxy.retry.retryable-after"), Value: defaults.Proxy.Retry.RetryableAfter},
		&cli.IntFlag{Name: "proxy.retry.max-attempts", Usage: flagHelp.MustUsage("proxy.retry.max-attempts"), Value: defaults.Proxy.Retry.MaxAttempts},
		&cli.DurationFlag{Name: "proxy.retry.command-retention", Usage: flagHelp.MustUsage("proxy.retry.command-retention"), Value: defaults.Proxy.Retry.CommandRetention},
		&cli.Int64Flag{Name: "proxy.body-memory.max-active-bytes", Usage: flagHelp.MustUsage("proxy.body-memory.max-active-bytes"), Value: defaults.Proxy.BodyMemory.MaxActiveBytes},
		&cli.Int64Flag{Name: "proxy.body-memory.max-body-bytes", Usage: flagHelp.MustUsage("proxy.body-memory.max-body-bytes"), Value: defaults.Proxy.BodyMemory.MaxBodyBytes},
		&cli.IntFlag{Name: "proxy.body-memory.queue-max-requests", Usage: flagHelp.MustUsage("proxy.body-memory.queue-max-requests"), Value: defaults.Proxy.BodyMemory.QueueMax},
		&cli.DurationFlag{Name: "proxy.body-memory.queue-max-wait", Usage: flagHelp.MustUsage("proxy.body-memory.queue-max-wait"), Value: defaults.Proxy.BodyMemory.QueueWait},
		&cli.DurationFlag{Name: "proxy.body-memory.body-read-timeout", Usage: flagHelp.MustUsage("proxy.body-memory.body-read-timeout"), Value: defaults.Proxy.BodyMemory.ReadTimeout},
		&cli.StringFlag{Name: "observability.instance-id", Usage: flagHelp.MustUsage("observability.instance-id"), Value: defaults.Observability.InstanceID},
		&cli.Int64Flag{Name: "proxy.directive.max-token-bytes", Usage: flagHelp.MustUsage("proxy.directive.max-token-bytes"), Value: defaults.Proxy.Directive.MaxTokenBytes},
		&cli.Int64Flag{Name: "proxy.directive.max-inline-bytes", Usage: flagHelp.MustUsage("proxy.directive.max-inline-bytes"), Value: defaults.Proxy.Directive.MaxInlineBytes},
		&cli.BoolFlag{Name: "proxy.directive.source-access.enabled", Usage: flagHelp.MustUsage("proxy.directive.source-access.enabled"), Value: defaults.Proxy.Directive.SourceAccess.Enabled},
		&cli.StringSliceFlag{Name: "proxy.directive.source-access.allowed-sources", Usage: flagHelp.MustUsage("proxy.directive.source-access.allowed-sources"), Value: defaults.Proxy.Directive.SourceAccess.AllowedSources},
		&cli.StringSliceFlag{Name: "proxy.directive.source-access.trusted-proxies", Usage: flagHelp.MustUsage("proxy.directive.source-access.trusted-proxies"), Value: defaults.Proxy.Directive.SourceAccess.TrustedProxies},
		&cli.DurationFlag{Name: "proxy.directive.source-access.dns.lookup-timeout", Usage: flagHelp.MustUsage("proxy.directive.source-access.dns.lookup-timeout"), Value: defaults.Proxy.Directive.SourceAccess.DNS.LookupTimeout},
		&cli.DurationFlag{Name: "proxy.directive.source-access.dns.success-ttl", Usage: flagHelp.MustUsage("proxy.directive.source-access.dns.success-ttl"), Value: defaults.Proxy.Directive.SourceAccess.DNS.SuccessTTL},
		&cli.DurationFlag{Name: "proxy.directive.source-access.dns.failure-ttl", Usage: flagHelp.MustUsage("proxy.directive.source-access.dns.failure-ttl"), Value: defaults.Proxy.Directive.SourceAccess.DNS.FailureTTL},
		&cli.DurationFlag{Name: "proxy.directive.source-access.dns.stale-ttl", Usage: flagHelp.MustUsage("proxy.directive.source-access.dns.stale-ttl"), Value: defaults.Proxy.Directive.SourceAccess.DNS.StaleTTL},
		&cli.IntFlag{Name: "proxy.directive.source-access.dns.max-hosts", Usage: flagHelp.MustUsage("proxy.directive.source-access.dns.max-hosts"), Value: defaults.Proxy.Directive.SourceAccess.DNS.MaxHosts},
		&cli.DurationFlag{Name: "proxy.directive.remote.timeout", Usage: flagHelp.MustUsage("proxy.directive.remote.timeout"), Value: defaults.Proxy.Directive.Remote.Timeout},
		&cli.Int64Flag{Name: "proxy.directive.remote.http.max-request-bytes", Usage: flagHelp.MustUsage("proxy.directive.remote.http.max-request-bytes"), Value: defaults.Proxy.Directive.Remote.HTTP.MaxRequestBytes},
		&cli.Int64Flag{Name: "proxy.directive.remote.max-response-bytes", Usage: flagHelp.MustUsage("proxy.directive.remote.max-response-bytes"), Value: defaults.Proxy.Directive.Remote.MaxResponseBytes},
		&cli.IntFlag{Name: "proxy.directive.remote.redis.client-cache-capacity", Usage: flagHelp.MustUsage("proxy.directive.remote.redis.client-cache-capacity"), Value: defaults.Proxy.Directive.Remote.Redis.ClientCacheCapacity},
		&cli.DurationFlag{Name: "proxy.directive.remote.redis.client-idle-timeout", Usage: flagHelp.MustUsage("proxy.directive.remote.redis.client-idle-timeout"), Value: defaults.Proxy.Directive.Remote.Redis.ClientIdleTimeout},
		&cli.IntFlag{Name: "proxy.directive.remote.redis.pool-size", Usage: flagHelp.MustUsage("proxy.directive.remote.redis.pool-size"), Value: defaults.Proxy.Directive.Remote.Redis.PoolSize},
	}
}

func authMethodFlagValues(methods []config.AuthMethod) []string {
	values := make([]string, len(methods))
	for index, method := range methods {
		values[index] = string(method)
	}
	return values
}
