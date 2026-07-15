package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

var (
	ErrActiveCapacity        = errors.New("proxy active request capacity is full")
	ErrResolverFailed        = errors.New("proxy directive resolver failed")
	ErrContentLengthRequired = errors.New("proxy request Content-Length is required")
	ErrBodyMemoryUnavailable = errors.New("proxy request body memory is unavailable")
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
	session, tracked := proxyrequest.SessionFromContext(req.Context())
	if !tracked || session == nil {
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
		attempt := session.BeginAttempt(cancel, source.Mode, source.Backend, source.Endpoint, source.Key)
		if attempt == 0 {
			cancel()
			return nil, ErrActiveCapacity
		}
		resolveStartedAt := time.Now()
		resolution, resolveErr := prepared.directive.ResolveAttempt(attemptCtx, attempt)
		resolveDuration := time.Since(resolveStartedAt)
		if resolveErr != nil {
			session.DirectiveFailed(attempt, resolveDuration, directiveErrorCode(resolveErr))
			session.FinishAttempt(attempt, false, resolveErr)
			cancel()
			return nil, resolveErr
		}
		if resolution.Plan == nil || resolution.Plan.Target == nil {
			session.DirectiveFailed(attempt, resolveDuration, "resolver_failed")
			session.FinishAttempt(attempt, false, ErrResolverFailed)
			cancel()
			return nil, ErrResolverFailed
		}
		normalizedMetadata, metadataErr := requestmeta.Normalize(resolution.Plan.Metadata)
		if metadataErr != nil {
			session.DirectiveFailed(attempt, resolveDuration, "resolver_failed")
			session.FinishAttempt(attempt, false, ErrResolverFailed)
			cancel()
			return nil, ErrResolverFailed
		}
		resolution.Plan.Metadata = normalizedMetadata
		if configureErr := session.ConfigureAttempt(attempt, resolution.Plan.PluginSpecs); configureErr != nil {
			pluginErr := error(ErrInvalidDirective)
			if source.Mode == "remote" {
				pluginErr = ErrRemoteDirectiveInvalid
			}
			session.DirectiveFailed(attempt, resolveDuration, "invalid_plugin_config")
			session.FinishAttempt(attempt, false, pluginErr)
			cancel()
			return nil, pluginErr
		}
		fingerprint := planFingerprint(resolution.Plan)
		planChanged := previousFingerprint != "" && previousFingerprint != fingerprint
		previousFingerprint = fingerprint
		target := BuildOutboundURL(resolution.Plan.Target, prepared.template.URL, resolution.Plan.JoinPath)
		targetValue := urlString(target)
		targetChanged := previousTarget != "" && previousTarget != targetValue
		previousTarget = targetValue
		session.BindMetadata(attempt, resolution.Plan.Metadata)
		session.DirectiveResolved(attempt, target, resolveDuration, resolution.Source.PayloadSHA256, targetChanged, planChanged)

		body := bodyLease.Reader()
		attemptRequest := BuildAttemptRequest(prepared.template, resolution.Plan, attemptCtx, body)
		if attemptRequest == nil {
			_ = body.Close()
			session.FinishAttempt(attempt, false, ErrResolverFailed)
			cancel()
			return nil, ErrResolverFailed
		}
		attemptRequest.Body = body
		attemptRequest.GetBody = func() (io.ReadCloser, error) { return bodyLease.Reader(), nil }
		attemptRequest.ContentLength = bodyLease.Size()
		attemptRequest.TransferEncoding = nil
		if !session.BeginUpstream(attempt, attemptRequest) {
			_ = body.Close()
			cancel()
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			return nil, context.Canceled
		}
		response, roundTripErr := t.base.RoundTrip(attemptRequest)
		action := session.FinishAttempt(attempt, response != nil && roundTripErr == nil, roundTripErr)
		if action == proxyrequest.AttemptRetry && req.Context().Err() == nil {
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
		session.ObserveUpstreamResponse(attempt, response)
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
		Target      string
		Proxy       string
		Headers     HeaderPlan
		Metadata    map[string][]string
		PluginSpecs map[string][]byte
		JoinPath    bool
	}{
		Target:      urlString(plan.Target),
		Proxy:       urlString(plan.Proxy),
		Headers:     plan.Headers,
		Metadata:    plan.Metadata,
		PluginSpecs: plan.PluginSpecs,
		JoinPath:    plan.JoinPath,
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
