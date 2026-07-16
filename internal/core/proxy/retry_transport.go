package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

var (
	ErrResolverFailed        = errors.New("proxy directive resolver failed")
	ErrContentLengthRequired = errors.New("proxy request Content-Length is required")
	ErrBodyMemoryUnavailable = errors.New("proxy request body memory is unavailable")
	ErrModuleFailed          = errors.New("proxy module failed")
)

type RetryTransportOptions struct{}

type RetryTransport struct{ base http.RoundTripper }

func (*RetryTransport) orchestratesPreparedRequests() {}

func NewRetryTransport(base http.RoundTripper, _ RetryTransportOptions) (*RetryTransport, error) {
	if base == nil {
		base = http.DefaultTransport
	}
	return &RetryTransport{base: base}, nil
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.base == nil || req == nil {
		return nil, errors.New("proxy retry transport is unavailable")
	}
	prepared, ok := preparedRequestFromContext(req.Context())
	if !ok {
		return t.base.RoundTrip(req)
	}
	current, tracked := exchange.FromContext(req.Context())
	if !tracked || current == nil {
		return t.roundTripOnce(req, prepared)
	}

	if prepared.body == nil {
		return nil, ErrBodyMemoryUnavailable
	}
	bodyLease := prepared.body.Acquire()
	if !bodyLease.Valid() {
		return nil, ErrBodyMemoryUnavailable
	}
	defer func() { _ = bodyLease.Close() }()
	var previousFingerprint string
	var previousTarget string
	for {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithCancel(req.Context())
		source := prepared.directive.Source()
		attempt, beginErr := current.BeginAttempt(cancel, exchange.AttemptSource{
			Mode: source.Mode, Backend: source.Backend, Endpoint: source.Endpoint, Key: source.Key,
		})
		if beginErr != nil {
			cancel()
			return nil, beginErr
		}
		attemptNumber := attempt.Number()
		resolveStartedAt := time.Now()
		resolution, resolveErr := prepared.directive.ResolveAttempt(attemptCtx, attemptNumber)
		resolveDuration := time.Since(resolveStartedAt)
		if resolveErr != nil {
			attempt.DirectiveFailed(resolveDuration, directiveErrorCode(resolveErr))
			attempt.FinishRoundTrip(false, resolveErr)
			cancel()
			return nil, resolveErr
		}
		if resolution.Plan == nil || resolution.Plan.Target == nil {
			attempt.DirectiveFailed(resolveDuration, "resolver_failed")
			attempt.FinishRoundTrip(false, ErrResolverFailed)
			cancel()
			return nil, ErrResolverFailed
		}
		normalizedMetadata, metadataErr := requestmeta.Normalize(resolution.Plan.Metadata)
		if metadataErr != nil {
			attempt.DirectiveFailed(resolveDuration, "resolver_failed")
			attempt.FinishRoundTrip(false, ErrResolverFailed)
			cancel()
			return nil, ErrResolverFailed
		}
		resolution.Plan.Metadata = normalizedMetadata
		if configureErr := attempt.ConfigureModules(resolution.Plan.Modules); configureErr != nil {
			moduleErr := error(ErrInvalidDirective)
			if source.Mode == "remote" {
				moduleErr = ErrRemoteDirectiveInvalid
			}
			attempt.DirectiveFailed(resolveDuration, "invalid_module_config")
			attempt.FinishRoundTrip(false, moduleErr)
			cancel()
			return nil, moduleErr
		}
		fingerprint := planFingerprint(resolution.Plan)
		planChanged := previousFingerprint != "" && previousFingerprint != fingerprint
		previousFingerprint = fingerprint
		target := BuildOutboundURL(resolution.Plan.Target, prepared.template.URL, resolution.Plan.JoinPath)
		targetValue := urlString(target)
		targetChanged := previousTarget != "" && previousTarget != targetValue
		previousTarget = targetValue
		attempt.BindMetadata(resolution.Plan.Metadata)
		attempt.DirectiveResolved(target, resolveDuration, resolution.Source.PayloadSHA256, targetChanged, planChanged)

		body := bodyLease.Reader()
		attemptRequest := BuildAttemptRequest(prepared.template, resolution.Plan, attemptCtx, body)
		if attemptRequest == nil {
			_ = body.Close()
			attempt.FinishRoundTrip(false, ErrResolverFailed)
			cancel()
			return nil, ErrResolverFailed
		}
		if mutationErr := attempt.MutateOutboundRequest(attemptRequest); mutationErr != nil {
			_ = body.Close()
			attempt.FinishRoundTrip(false, mutationErr)
			cancel()
			return nil, fmt.Errorf("%w: outbound request: %v", ErrModuleFailed, mutationErr)
		}
		bodyData, mutationErr := attempt.MutateOutboundBody(bodyLease.Bytes())
		if mutationErr != nil {
			_ = body.Close()
			attempt.FinishRoundTrip(false, mutationErr)
			cancel()
			return nil, fmt.Errorf("%w: outbound body: %v", ErrModuleFailed, mutationErr)
		}
		_ = body.Close()
		attemptRequest.Body = io.NopCloser(bytes.NewReader(bodyData))
		attemptRequest.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyData)), nil
		}
		attemptRequest.ContentLength = int64(len(bodyData))
		attemptRequest.TransferEncoding = nil
		if !attempt.BeginUpstream(attemptRequest) {
			_ = body.Close()
			cancel()
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			return nil, context.Canceled
		}
		response, roundTripErr := t.base.RoundTrip(attemptRequest)
		if roundTripErr == nil && response != nil {
			if mutationErr := attempt.MutateUpstreamResponse(response); mutationErr != nil {
				if response.Body != nil {
					_ = response.Body.Close()
				}
				attempt.FinishRoundTrip(false, mutationErr)
				cancel()
				return nil, fmt.Errorf("%w: upstream response: %v", ErrModuleFailed, mutationErr)
			}
		}
		decision := attempt.FinishRoundTrip(response != nil && roundTripErr == nil, roundTripErr)
		if decision == exchange.DecisionRetry && req.Context().Err() == nil {
			cancel()
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			continue
		}
		if roundTripErr != nil || response == nil || response.Body == nil {
			cancel()
			return response, roundTripErr
		}
		attempt.ObserveUpstreamResponse(response)
		bindResponseHeaderPlan(response, attemptRequest, resolution.Plan.Headers.Response)
		response.Body = wrapCancelOnCloseBody(response, cancel)
		return response, roundTripErr
	}
}

func (t *RetryTransport) roundTripOnce(req *http.Request, prepared preparedRequest) (*http.Response, error) {
	resolution, err := prepared.directive.ResolveAttempt(req.Context(), 1)
	if err != nil {
		return nil, err
	}
	if resolution.Plan == nil || resolution.Plan.Target == nil {
		return nil, ErrResolverFailed
	}
	normalizedMetadata, metadataErr := requestmeta.Normalize(resolution.Plan.Metadata)
	if metadataErr != nil {
		return nil, ErrResolverFailed
	}
	resolution.Plan.Metadata = normalizedMetadata
	if prepared.body == nil {
		return nil, ErrBodyMemoryUnavailable
	}
	bodyLease := prepared.body.Acquire()
	if !bodyLease.Valid() {
		return nil, ErrBodyMemoryUnavailable
	}
	defer func() { _ = bodyLease.Close() }()
	body := bodyLease.Reader()
	attemptRequest := BuildAttemptRequest(prepared.template, resolution.Plan, req.Context(), body)
	if attemptRequest == nil {
		return nil, ErrResolverFailed
	}
	attemptRequest.GetBody = func() (io.ReadCloser, error) { return bodyLease.Reader(), nil }
	attemptRequest.ContentLength = bodyLease.Size()
	attemptRequest.TransferEncoding = nil
	response, roundTripErr := t.base.RoundTrip(attemptRequest)
	if response != nil {
		bindResponseHeaderPlan(response, attemptRequest, resolution.Plan.Headers.Response)
	}
	return response, roundTripErr
}

func directiveErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidDirective):
		return "invalid_directive"
	case errors.Is(err, ErrDirectiveTokenTooLarge):
		return "directive_token_too_large"
	case errors.Is(err, ErrDirectiveNotFound):
		return "directive_not_found"
	case errors.Is(err, ErrRemoteDirectiveUnavailable):
		return "remote_unavailable"
	case errors.Is(err, ErrDirectiveMetadataTooLarge):
		return "request_metadata_too_large"
	case errors.Is(err, ErrRemoteDirectiveInvalid):
		return "remote_response_invalid"
	default:
		return "resolver_failed"
	}
}

func planFingerprint(plan *Plan) string {
	if plan == nil {
		return ""
	}
	data, err := json.Marshal(struct {
		Target   string
		Proxy    string
		Headers  HeaderPlan
		Metadata map[string][]string
		Modules  []module.Spec
		JoinPath bool
	}{
		Target:   urlString(plan.Target),
		Proxy:    urlString(plan.Proxy),
		Headers:  plan.Headers,
		Metadata: plan.Metadata,
		Modules:  plan.Modules,
		JoinPath: plan.JoinPath,
	})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func urlString(value *url.URL) string {
	if value == nil {
		return ""
	}
	return value.String()
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	done   atomic.Bool
}

type cancelOnCloseReadWriteBody struct {
	*cancelOnCloseBody
	writer io.Writer
}

func wrapCancelOnCloseBody(response *http.Response, cancel context.CancelFunc) io.ReadCloser {
	body := &cancelOnCloseBody{ReadCloser: response.Body, cancel: cancel}
	if response.StatusCode == http.StatusSwitchingProtocols {
		if writer, ok := response.Body.(io.Writer); ok {
			return &cancelOnCloseReadWriteBody{cancelOnCloseBody: body, writer: writer}
		}
	}
	return body
}

func (b *cancelOnCloseReadWriteBody) Write(data []byte) (int, error) {
	return b.writer.Write(data)
}

func (b *cancelOnCloseBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.finish()
	return err
}

func (b *cancelOnCloseBody) finish() {
	if b != nil && b.done.CompareAndSwap(false, true) && b.cancel != nil {
		b.cancel()
	}
}
