package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
	"github.com/lwmacct/251207-go-pkg-version/pkg/version"
	"github.com/lwmacct/251219-go-pkg-logm/pkg/logm"

	"github.com/lwmacct/260628-directive-proxy/internal/appcmd/server"
)

func main() {
	logm.MustInit(logm.PresetAuto())
	defer func() { _ = logm.Close() }()

	cmd := &cli.Command{
		Name:            "app",
		Usage:           "directive-driven HTTP reverse proxy",
		Version:         version.AppVersion,
		Flags:           []cli.Flag{cfgm.ConfigFlag()},
		Commands:        []*cli.Command{server.Command, version.Command},
		HideHelpCommand: true,
		Action: func(ctx context.Context, c *cli.Command) error {
			return cli.ShowSubcommandHelp(c)
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
