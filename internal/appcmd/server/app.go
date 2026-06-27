package server

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/config"

type App struct {
	cfg *config.Config
}

func NewApp(cfg *config.Config) *App {
	return &App{cfg: cfg}
}
