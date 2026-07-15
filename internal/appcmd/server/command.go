package server

import (
	"context"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
	"github.com/lwmacct/251207-go-pkg-version/pkg/version"
	"github.com/urfave/cli/v3"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
)

var (
	binding = config.Definition.Bind(
		cfgm.Command("server"),
		cfgm.NoCLI(
			"http.auth.session.keys",
			"http.auth.token",
		),
	)
)

var Command = &cli.Command{
	Name:            "server",
	Usage:           "start directive proxy server",
	Action:          action,
	Commands:        []*cli.Command{version.Command},
	HideHelpCommand: true,
	Flags:           binding.Flags(),
}

func action(ctx context.Context, cmd *cli.Command) error {
	cfg := binding.MustLoad(ctx, cmd)
	return NewApp(&cfg.Server).Run(ctx)
}
