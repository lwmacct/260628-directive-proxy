package handler

import "github.com/danielgtaylor/huma/v2"

func utilOptionalInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func utilOptionalInt64(value *int64) int64 {
	if value == nil {
		return -1
	}
	return *value
}

func utilHTTPConfig() huma.Config {
	config := huma.DefaultConfig("LLM Relay Directive Proxy", "1.0.0")
	config.OpenAPIPath = "/openapi.json"
	config.DocsPath = "/docs"
	config.SchemasPath = "/schemas"
	return config
}
