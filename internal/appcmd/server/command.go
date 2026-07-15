package server

import (
	"context"

	"github.com/lwmacct/251207-go-pkg-version/pkg/version"
	"github.com/urfave/cli/v3"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
)

var Command = &cli.Command{
	Name:            "server",
	Usage:           "start directive proxy server",
	Action:          config.Manager.Action(action),
	Commands:        []*cli.Command{version.Command},
	HideHelpCommand: true,
}

func action(ctx context.Context, _ *cli.Command, cfg *config.Config) error {
	return NewApp(&cfg.Server).Run(ctx)
}
