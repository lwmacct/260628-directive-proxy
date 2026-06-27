package handler

import "github.com/danielgtaylor/huma/v2"

func utilHTTPConfig() huma.Config {
	config := huma.DefaultConfig("LLM Relay Directive Proxy", "1.0.0")
	config.OpenAPIPath = "/openapi.json"
	config.DocsPath = "/docs"
	config.SchemasPath = "/schemas"
	return config
}
