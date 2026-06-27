package server

import (
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

func commandFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "server.debug", Usage: flagHelp.MustUsage("server.debug"), Value: defaults.Server.Debug},
		&cli.StringFlag{Name: "server.http.listen", Usage: flagHelp.MustUsage("server.http.listen"), Value: defaults.Server.HTTP.Listen},
		&cli.BoolFlag{Name: "server.http.tls.enabled", Usage: flagHelp.MustUsage("server.http.tls.enabled"), Value: defaults.Server.HTTP.TLS.Enabled},
		&cli.StringFlag{Name: "server.http.tls.cert-file", Usage: flagHelp.MustUsage("server.http.tls.cert-file"), Value: defaults.Server.HTTP.TLS.CertFile},
		&cli.StringFlag{Name: "server.http.tls.key-file", Usage: flagHelp.MustUsage("server.http.tls.key-file"), Value: defaults.Server.HTTP.TLS.KeyFile},
		&cli.BoolFlag{Name: "server.http.tls.auto-reload", Usage: flagHelp.MustUsage("server.http.tls.auto-reload"), Value: defaults.Server.HTTP.TLS.AutoReload},
		&cli.DurationFlag{Name: "server.http.tls.reload-interval", Usage: flagHelp.MustUsage("server.http.tls.reload-interval"), Value: defaults.Server.HTTP.TLS.ReloadInterval},
		&cli.DurationFlag{Name: "server.http.read-timeout", Usage: flagHelp.MustUsage("server.http.read-timeout"), Value: defaults.Server.HTTP.ReadTimeout},
		&cli.DurationFlag{Name: "server.http.write-timeout", Usage: flagHelp.MustUsage("server.http.write-timeout"), Value: defaults.Server.HTTP.WriteTimeout},
		&cli.DurationFlag{Name: "server.http.idle-timeout", Usage: flagHelp.MustUsage("server.http.idle-timeout"), Value: defaults.Server.HTTP.IdleTimeout},
		&cli.Int64Flag{Name: "server.http.max-api-body-bytes", Usage: flagHelp.MustUsage("server.http.max-api-body-bytes"), Value: defaults.Server.HTTP.MaxAPIBodyBytes},
		&cli.StringFlag{Name: "proxy.path-prefix", Usage: flagHelp.MustUsage("proxy.path-prefix"), Value: defaults.Proxy.PathPrefix},
		&cli.IntFlag{Name: "proxy.transport.max-idle-conns", Usage: flagHelp.MustUsage("proxy.transport.max-idle-conns"), Value: defaults.Proxy.Transport.MaxIdleConns},
		&cli.IntFlag{Name: "proxy.transport.max-idle-conns-per-host", Usage: flagHelp.MustUsage("proxy.transport.max-idle-conns-per-host"), Value: defaults.Proxy.Transport.MaxIdleConnsPerHost},
		&cli.IntFlag{Name: "proxy.transport.max-conns-per-host", Usage: flagHelp.MustUsage("proxy.transport.max-conns-per-host"), Value: defaults.Proxy.Transport.MaxConnsPerHost},
		&cli.DurationFlag{Name: "proxy.transport.idle-conn-timeout", Usage: flagHelp.MustUsage("proxy.transport.idle-conn-timeout"), Value: defaults.Proxy.Transport.IdleConnTimeout},
		&cli.BoolFlag{Name: "proxy.transport.disable-keep-alives", Usage: flagHelp.MustUsage("proxy.transport.disable-keep-alives"), Value: defaults.Proxy.Transport.DisableKeepAlives},
		&cli.BoolFlag{Name: "event.kafka.enabled", Usage: flagHelp.MustUsage("event.kafka.enabled"), Value: defaults.Event.Kafka.Enabled},
		&cli.BoolFlag{Name: "event.kafka.capture-abnormal", Usage: flagHelp.MustUsage("event.kafka.capture-abnormal"), Value: defaults.Event.Kafka.CaptureAbnormal},
		&cli.BoolFlag{Name: "event.kafka.ensure-topics", Usage: flagHelp.MustUsage("event.kafka.ensure-topics"), Value: defaults.Event.Kafka.EnsureTopics},
		&cli.StringFlag{Name: "event.kafka.brokers", Usage: flagHelp.MustUsage("event.kafka.brokers"), Value: defaults.Event.Kafka.Brokers},
		&cli.StringFlag{Name: "event.kafka.topic-prefix", Usage: flagHelp.MustUsage("event.kafka.topic-prefix"), Value: defaults.Event.Kafka.TopicPrefix},
		&cli.DurationFlag{Name: "event.kafka.publish-timeout", Usage: flagHelp.MustUsage("event.kafka.publish-timeout"), Value: defaults.Event.Kafka.PublishTimeout},
		&cli.IntFlag{Name: "event.kafka.max-publish-retries", Usage: flagHelp.MustUsage("event.kafka.max-publish-retries"), Value: defaults.Event.Kafka.MaxPublishRetries},
		&cli.StringFlag{Name: "event.kafka.sasl.username", Usage: flagHelp.MustUsage("event.kafka.sasl.username"), Value: defaults.Event.Kafka.SASL.Username},
		&cli.StringFlag{Name: "event.kafka.sasl.password", Usage: flagHelp.MustUsage("event.kafka.sasl.password"), Value: defaults.Event.Kafka.SASL.Password},
		&cli.BoolFlag{Name: "plugins.usage.enabled", Usage: flagHelp.MustUsage("plugins.usage.enabled"), Value: defaults.Plugins.Usage.Enabled},
		&cli.StringFlag{Name: "plugins.usage.mode", Usage: flagHelp.MustUsage("plugins.usage.mode"), Value: defaults.Plugins.Usage.Mode},
		&cli.StringSliceFlag{Name: "plugins.usage.fields", Usage: flagHelp.MustUsage("plugins.usage.fields"), Value: defaults.Plugins.Usage.Fields},
		&cli.BoolFlag{Name: "plugins.usage.delivery.enabled", Usage: flagHelp.MustUsage("plugins.usage.delivery.enabled"), Value: defaults.Plugins.Usage.Delivery.Enabled},
		&cli.BoolFlag{Name: "plugins.usage.delivery.kafka", Usage: flagHelp.MustUsage("plugins.usage.delivery.kafka"), Value: defaults.Plugins.Usage.Delivery.Kafka},
		&cli.StringFlag{Name: "plugins.usage.delivery.url", Usage: flagHelp.MustUsage("plugins.usage.delivery.url"), Value: defaults.Plugins.Usage.Delivery.URL},
		&cli.StringFlag{Name: "plugins.usage.delivery.token", Usage: flagHelp.MustUsage("plugins.usage.delivery.token"), Value: defaults.Plugins.Usage.Delivery.Token},
		&cli.IntFlag{Name: "plugins.usage.delivery.max-backlog", Usage: flagHelp.MustUsage("plugins.usage.delivery.max-backlog"), Value: defaults.Plugins.Usage.Delivery.MaxBacklog},
		&cli.DurationFlag{Name: "plugins.usage.delivery.flush-interval", Usage: flagHelp.MustUsage("plugins.usage.delivery.flush-interval"), Value: defaults.Plugins.Usage.Delivery.FlushInterval},
		&cli.IntFlag{Name: "plugins.usage.delivery.batch-size", Usage: flagHelp.MustUsage("plugins.usage.delivery.batch-size"), Value: defaults.Plugins.Usage.Delivery.BatchSize},
		&cli.DurationFlag{Name: "plugins.usage.delivery.timeout", Usage: flagHelp.MustUsage("plugins.usage.delivery.timeout"), Value: defaults.Plugins.Usage.Delivery.Timeout},
	}
}
