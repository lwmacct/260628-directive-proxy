package server

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/config"

func validateConfig(cfg *config.Config) error {
	if cfg == nil {
		return config.ErrInvalidHTTP
	}
	validated, err := config.Validate(*cfg)
	if err != nil {
		return err
	}
	*cfg = validated
	return nil
}
