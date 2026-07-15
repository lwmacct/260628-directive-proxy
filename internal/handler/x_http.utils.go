package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-directive-proxy/internal/core/apierror"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

func utilNowUTC() time.Time { return time.Now().UTC() }

func utilHTTPConfig(title, prefix string) huma.Config {
	config := huma.DefaultConfig(title, "1.0.0")
	if prefix == "" {
		config.OpenAPIPath = ""
		config.DocsPath = ""
		config.SchemasPath = ""
		return config
	}
	config.OpenAPIPath = prefix + "/openapi.json"
	config.DocsPath = prefix + "/docs"
	config.SchemasPath = prefix + "/schemas"
	return config
}

func utilNewAPIError(status int, code, message string) *apierror.Error {
	return apierror.New(status, code, message)
}

func utilRetryAPIError(err error) error {
	switch {
	case errors.Is(err, proxyrequest.ErrNotFound):
		return utilNewAPIError(http.StatusNotFound, "proxy_request_not_found", "proxy request was not found")
	case errors.Is(err, proxyrequest.ErrAttemptChanged):
		return utilNewAPIError(http.StatusConflict, "attempt_changed", "proxy request attempt changed")
	case errors.Is(err, proxyrequest.ErrRetryNotReady):
		return utilNewAPIError(http.StatusConflict, "retry_not_ready", "proxy request is not ready for retry")
	case errors.Is(err, proxyrequest.ErrMaxAttempts):
		return utilNewAPIError(http.StatusTooManyRequests, "max_attempts_reached", "proxy request maximum attempts reached")
	case errors.Is(err, proxyrequest.ErrIdempotencyKeyRequired):
		return utilNewAPIError(http.StatusUnprocessableEntity, "idempotency_key_required", "Idempotency-Key is required to retry POST or PATCH requests")
	default:
		return utilNewAPIError(http.StatusConflict, "request_state_changed", "proxy request state changed")
	}
}
