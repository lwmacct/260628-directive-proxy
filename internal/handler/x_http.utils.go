package handler

import (
	"time"

	"github.com/danielgtaylor/huma/v2"
)

func utilNowUTC() time.Time { return time.Now().UTC() }

func utilHTTPConfig() huma.Config {
	config := huma.DefaultConfig("Directive Proxy", "1.0.0")
	config.OpenAPIPath = "/openapi.json"
	config.DocsPath = "/docs"
	config.SchemasPath = "/schemas"
	return config
}
