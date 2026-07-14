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
		&cli.IntFlag{Name: "proxy.retry.max-active-requests", Usage: flagHelp.MustUsage("proxy.retry.max-active-requests"), Value: defaults.Proxy.Retry.MaxActiveRequests},
		&cli.StringFlag{Name: "proxy.retry.temp-dir", Usage: flagHelp.MustUsage("proxy.retry.temp-dir"), Value: defaults.Proxy.Retry.TempDir},
		&cli.Int64Flag{Name: "proxy.retry.max-body-bytes", Usage: flagHelp.MustUsage("proxy.retry.max-body-bytes"), Value: defaults.Proxy.Retry.MaxBodyBytes},
		&cli.Int64Flag{Name: "proxy.retry.max-inflight-bytes", Usage: flagHelp.MustUsage("proxy.retry.max-inflight-bytes"), Value: defaults.Proxy.Retry.MaxInflightBytes},
		&cli.BoolFlag{Name: "proxy.capture.enabled", Usage: flagHelp.MustUsage("proxy.capture.enabled"), Value: defaults.Proxy.Capture.Enabled},
		&cli.StringFlag{Name: "proxy.capture.instance-id", Usage: flagHelp.MustUsage("proxy.capture.instance-id"), Value: defaults.Proxy.Capture.InstanceID},
		&cli.IntFlag{Name: "proxy.capture.body-chunk-bytes", Usage: flagHelp.MustUsage("proxy.capture.body-chunk-bytes"), Value: defaults.Proxy.Capture.BodyChunkBytes},
		&cli.IntFlag{Name: "proxy.capture.max-sse-event-bytes", Usage: flagHelp.MustUsage("proxy.capture.max-sse-event-bytes"), Value: defaults.Proxy.Capture.MaxSSEEventBytes},
		&cli.StringSliceFlag{Name: "proxy.capture.redact-headers", Usage: flagHelp.MustUsage("proxy.capture.redact-headers"), Value: defaults.Proxy.Capture.RedactHeaders},
		&cli.StringSliceFlag{Name: "proxy.capture.redact-query", Usage: flagHelp.MustUsage("proxy.capture.redact-query"), Value: defaults.Proxy.Capture.RedactQuery},
		&cli.StringFlag{Name: "proxy.capture.fluent.endpoint", Usage: flagHelp.MustUsage("proxy.capture.fluent.endpoint"), Value: defaults.Proxy.Capture.Fluent.Endpoint},
		&cli.IntFlag{Name: "proxy.capture.fluent.connections", Usage: flagHelp.MustUsage("proxy.capture.fluent.connections"), Value: defaults.Proxy.Capture.Fluent.Connections},
		&cli.IntFlag{Name: "proxy.capture.fluent.queue-capacity", Usage: flagHelp.MustUsage("proxy.capture.fluent.queue-capacity"), Value: defaults.Proxy.Capture.Fluent.QueueCapacity},
		&cli.DurationFlag{Name: "proxy.capture.fluent.connect-timeout", Usage: flagHelp.MustUsage("proxy.capture.fluent.connect-timeout"), Value: defaults.Proxy.Capture.Fluent.ConnectTimeout},
		&cli.DurationFlag{Name: "proxy.capture.fluent.handshake-timeout", Usage: flagHelp.MustUsage("proxy.capture.fluent.handshake-timeout"), Value: defaults.Proxy.Capture.Fluent.HandshakeTimeout},
		&cli.DurationFlag{Name: "proxy.capture.fluent.write-timeout", Usage: flagHelp.MustUsage("proxy.capture.fluent.write-timeout"), Value: defaults.Proxy.Capture.Fluent.WriteTimeout},
		&cli.DurationFlag{Name: "proxy.capture.fluent.ack-timeout", Usage: flagHelp.MustUsage("proxy.capture.fluent.ack-timeout"), Value: defaults.Proxy.Capture.Fluent.ACKTimeout},
		&cli.IntFlag{Name: "proxy.capture.fluent.retry-max-attempts", Usage: flagHelp.MustUsage("proxy.capture.fluent.retry-max-attempts"), Value: defaults.Proxy.Capture.Fluent.RetryMaxAttempts},
		&cli.DurationFlag{Name: "proxy.capture.fluent.retry-min-backoff", Usage: flagHelp.MustUsage("proxy.capture.fluent.retry-min-backoff"), Value: defaults.Proxy.Capture.Fluent.RetryMinBackoff},
		&cli.DurationFlag{Name: "proxy.capture.fluent.retry-max-backoff", Usage: flagHelp.MustUsage("proxy.capture.fluent.retry-max-backoff"), Value: defaults.Proxy.Capture.Fluent.RetryMaxBackoff},
		&cli.StringFlag{Name: "proxy.capture.fluent.tag-prefix", Usage: flagHelp.MustUsage("proxy.capture.fluent.tag-prefix"), Value: defaults.Proxy.Capture.Fluent.TagPrefix},
		&cli.StringFlag{Name: "proxy.capture.fluent.delivery", Usage: flagHelp.MustUsage("proxy.capture.fluent.delivery"), Value: defaults.Proxy.Capture.Fluent.Delivery},
		&cli.BoolFlag{Name: "proxy.capture.fluent.tls-insecure-skip-verify", Usage: flagHelp.MustUsage("proxy.capture.fluent.tls-insecure-skip-verify"), Value: defaults.Proxy.Capture.Fluent.TLSInsecureSkipVerify},
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
