package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/lwmacct/251219-go-pkg-logm/pkg/logm"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/appcmd/server"
)

func main() {
	logm.MustInit(logm.PresetAuto())
	defer func() { _ = logm.Close() }()

	if err := server.Command.Run(context.Background(), os.Args); err != nil {
		slog.Error("llm relay directive proxy failed", "error", err)
		os.Exit(1)
	}
}
