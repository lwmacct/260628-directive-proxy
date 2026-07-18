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
	"net/http/httptrace"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

var (
	ErrResolverFailed       = errors.New("proxy directive resolver failed")
	ErrBodyStoreUnavailable = errors.New("proxy request body replay store is unavailable")
	ErrModuleFailed         = errors.New("proxy module failed")
	ErrRecoveryFailed       = errors.New("proxy recovery failed")
)

type RecoveryTransportOptions struct {
	RecoveryController         recovery.Controller
	MaxRecoveryAttempts        int
	MaxRecoveryElapsed         time.Duration
	MaxRecoveryCallbackTimeout time.Duration
	MaxRecoveryBodyBytes       int64
}

type RecoveryTransport struct {
	base                       http.RoundTripper
	recoveryController         recovery.Controller
	maxRecoveryAttempts        int
	maxRecoveryElapsed         time.Duration
	maxRecoveryCallbackTimeout time.Duration
	maxRecoveryBodyBytes       int64
}

func (*RecoveryTransport) orchestratesPreparedRequests() {}

func NewRecoveryTransport(base http.RoundTripper, options RecoveryTransportOptions) (*RecoveryTransport, error) {
	if base == nil {
		base = http.DefaultTransport
	}
	return &RecoveryTransport{
		base: base, recoveryController: options.RecoveryController,
		maxRecoveryAttempts: options.MaxRecoveryAttempts, maxRecoveryElapsed: options.MaxRecoveryElapsed,
		maxRecoveryCallbackTimeout: options.MaxRecoveryCallbackTimeout, maxRecoveryBodyBytes: options.MaxRecoveryBodyBytes,
	}, nil
}

func (t *RecoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.base == nil || req == nil {
		return nil, errors.New("proxy recovery transport is unavailable")
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
		return nil, ErrBodyStoreUnavailable
	}
	recoveryPolicy := t.limitRecoveryPolicy(PreparedRecovery(prepared.directive))
	if recoveryPolicy != nil {
		current.ConfigureRecovery(recoveryPolicy, t.maxRecoveryAttempts, t.maxRecoveryElapsed)
	}
	var previousFingerprint string
	var previousTarget string
	for {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithCancel(req.Context())
		source := prepared.directive.Source()
		attempt, beginErr := current.BeginAttempt(cancel, exchange.AttemptSource{
			Mode: source.Mode, Backend: source.Backend, Endpoint: source.Endpoint, Resource: source.Resource,
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
		target := cloneURL(resolution.Plan.Target)
		targetValue := urlString(target)
		targetChanged := previousTarget != "" && previousTarget != targetValue
		previousTarget = targetValue
		attempt.BindMetadata(resolution.Plan.Metadata)
		attempt.DirectiveResolved(target, resolveDuration, resolution.Source.PayloadSHA256, targetChanged, planChanged)

		body, bodyErr := prepared.body.Open(attemptCtx)
		if bodyErr != nil {
			attempt.FinishRoundTrip(false, bodyErr)
			cancel()
			return nil, bodyErr
		}
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
		if attempt.HasOutboundBodyMutators() {
			attemptRequest.Body = newMutatingBody(body, func(data []byte) ([]byte, error) {
				mutated, err := attempt.MutateOutboundBodyChunk(data)
				if err != nil {
					return nil, fmt.Errorf("%w: outbound body chunk: %v", ErrModuleFailed, err)
				}
				return mutated, nil
			})
			attemptRequest.ContentLength = -1
		} else {
			attemptRequest.ContentLength = prepared.body.ContentLength()
		}
		attemptRequest.GetBody = nil
		attemptRequest.TransferEncoding = nil
		if !attempt.BeginUpstream(attemptRequest) {
			_ = body.Close()
			cancel()
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			return nil, context.Canceled
		}
		response, roundTripErr, responseTimedOut := t.roundTrip(attemptRequest, recoveryPolicy)
		if roundTripErr != nil {
			trigger := recovery.Trigger{Type: recovery.TriggerTransportError, Code: "transport_error"}
			enabled := recoveryPolicy != nil && recoveryPolicy.Triggers.TransportError
			if responseTimedOut {
				trigger = recovery.Trigger{
					Type:      recovery.TriggerResponseHeaderTimeout,
					TimeoutMS: recoveryPolicy.Triggers.ResponseHeaderTimeout.Milliseconds(),
				}
				enabled = recoveryPolicy != nil && recoveryPolicy.Triggers.ResponseHeaderTimeout > 0
			}
			if enabled {
				recoveryResult, started, recoveryErr := t.recoverAttempt(req.Context(), recoveryPolicy, attempt, resolution.Source, trigger, nil)
				if started && recoveryErr == nil && recoveryResult.Retry {
					attempt.FinishRoundTrip(false, context.Canceled)
					cancel()
					continue
				}
				if errors.Is(recoveryErr, ErrRecoveryFailed) {
					attempt.FinishRoundTrip(false, ErrRecoveryFailed)
					cancel()
					return nil, ErrRecoveryFailed
				}
			}
		}
		if roundTripErr == nil && response != nil && recoveryPolicy != nil &&
			recoveryPolicy.Triggers.UnexpectedStatus.Matches(response.StatusCode) {
			captured, captureErr := captureRecoveryResponse(response, recoveryPolicy.Triggers.UnexpectedStatus.CaptureBodyBytes)
			if captureErr != nil {
				if response.Body != nil {
					_ = response.Body.Close()
				}
				attempt.FinishRoundTrip(false, captureErr)
				cancel()
				return nil, captureErr
			}
			recoveryResult, started, recoveryErr := t.recoverAttempt(req.Context(), recoveryPolicy, attempt, resolution.Source,
				recovery.Trigger{Type: recovery.TriggerUnexpectedStatus}, captured)
			if started && recoveryErr == nil && recoveryResult.Retry {
				_ = response.Body.Close()
				attempt.FinishRoundTrip(false, context.Canceled)
				cancel()
				continue
			}
			if errors.Is(recoveryErr, ErrRecoveryFailed) {
				_ = response.Body.Close()
				attempt.FinishRoundTrip(false, ErrRecoveryFailed)
				cancel()
				return nil, ErrRecoveryFailed
			}
		}
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
			prepared.body.Retire()
			cancel()
			return response, roundTripErr
		}
		attempt.ObserveUpstreamResponse(response)
		bindResponseHeaderPlan(response, attemptRequest, resolution.Plan.Headers.Response)
		response.Body = wrapCancelOnCloseBody(response, cancel)
		prepared.body.Retire()
		return response, roundTripErr
	}
}

func (t *RecoveryTransport) limitRecoveryPolicy(policy *recovery.Policy) *recovery.Policy {
	policy = recovery.ClonePolicy(policy)
	if policy == nil {
		return nil
	}
	if t.maxRecoveryAttempts > 0 && policy.Budget.MaxAttempts > t.maxRecoveryAttempts {
		policy.Budget.MaxAttempts = t.maxRecoveryAttempts
	}
	if t.maxRecoveryElapsed > 0 && policy.Budget.MaxElapsed > t.maxRecoveryElapsed {
		policy.Budget.MaxElapsed = t.maxRecoveryElapsed
	}
	if t.maxRecoveryCallbackTimeout > 0 && policy.Controller.Timeout > t.maxRecoveryCallbackTimeout {
		policy.Controller.Timeout = t.maxRecoveryCallbackTimeout
	}
	if policy.Triggers.UnexpectedStatus != nil && t.maxRecoveryBodyBytes > 0 &&
		policy.Triggers.UnexpectedStatus.CaptureBodyBytes > t.maxRecoveryBodyBytes {
		policy.Triggers.UnexpectedStatus.CaptureBodyBytes = t.maxRecoveryBodyBytes
	}
	return policy
}

func (t *RecoveryTransport) roundTrip(request *http.Request, policy *recovery.Policy) (*http.Response, error, bool) {
	if policy == nil || policy.Triggers.ResponseHeaderTimeout <= 0 {
		response, err := t.base.RoundTrip(request)
		return response, err, false
	}
	ctx, cancel := context.WithCancel(request.Context())
	var timedOut atomic.Bool
	var timerMu sync.Mutex
	var timer *time.Timer
	trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) {
		timerMu.Lock()
		if timer == nil {
			timer = time.AfterFunc(policy.Triggers.ResponseHeaderTimeout, func() {
				timedOut.Store(true)
				cancel()
			})
		}
		timerMu.Unlock()
	}}
	traced := request.WithContext(httptrace.WithClientTrace(ctx, trace))
	response, err := t.base.RoundTrip(traced)
	timerMu.Lock()
	if timer != nil {
		timer.Stop()
	}
	timerMu.Unlock()
	if err != nil || response == nil {
		cancel()
	}
	return response, err, timedOut.Load()
}

func (t *RecoveryTransport) recoverAttempt(ctx context.Context, policy *recovery.Policy, attempt *exchange.Attempt, source SourceMetadata,
	trigger recovery.Trigger, response *recovery.Response,
) (exchange.RecoveryResult, bool, error) {
	if t == nil || policy == nil || attempt == nil {
		return exchange.RecoveryResult{}, false, nil
	}
	info := attempt.RecoveryContext()
	cycle, err := exchange.NewRecoveryCycle(attempt, policy, t.recoveryController, exchange.RecoveryInput{
		Trigger: trigger,
		Directive: recovery.DirectiveInfo{
			Mode: source.Mode, Backend: source.Backend, Endpoint: source.Endpoint,
			Resource: source.Resource, PayloadSHA256: source.PayloadSHA256,
		},
		Metadata: info.Metadata, Response: response,
	})
	if errors.Is(err, exchange.ErrRecoveryNotStarted) || errors.Is(err, exchange.ErrRecoveryFailed) {
		return exchange.RecoveryResult{}, false, nil
	}
	if err != nil {
		return exchange.RecoveryResult{}, true, err
	}
	if _, err := cycle.Decide(ctx); err != nil {
		return exchange.RecoveryResult{}, true, err
	}
	result, err := cycle.Apply()
	if errors.Is(err, exchange.ErrRecoveryFailed) {
		return result, true, ErrRecoveryFailed
	}
	return result, true, err
}

func captureRecoveryResponse(response *http.Response, limit int64) (*recovery.Response, error) {
	if response == nil || response.Body == nil || limit <= 0 {
		return nil, ErrRecoveryFailed
	}
	read, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	truncated := int64(len(read)) > limit
	captured := read
	if truncated {
		captured = read[:limit]
		response.Body = &joinedResponseBody{Reader: io.MultiReader(bytes.NewReader(read), response.Body), source: response.Body}
	} else {
		_ = response.Body.Close()
		response.Body = io.NopCloser(bytes.NewReader(read))
	}
	size := int64(len(read))
	if response.ContentLength >= 0 {
		size = response.ContentLength
	}
	return &recovery.Response{
		StatusCode: response.StatusCode, Headers: response.Header.Clone(), Body: recovery.NewCapturedBody(captured, size, truncated),
	}, nil
}

type joinedResponseBody struct {
	io.Reader
	source io.Closer
}

func (body *joinedResponseBody) Close() error {
	if body == nil || body.source == nil {
		return nil
	}
	return body.source.Close()
}

func (t *RecoveryTransport) roundTripOnce(req *http.Request, prepared preparedRequest) (*http.Response, error) {
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
		return nil, ErrBodyStoreUnavailable
	}
	body, bodyErr := prepared.body.Open(req.Context())
	if bodyErr != nil {
		return nil, bodyErr
	}
	attemptRequest := BuildAttemptRequest(prepared.template, resolution.Plan, req.Context(), body)
	if attemptRequest == nil {
		_ = body.Close()
		return nil, ErrResolverFailed
	}
	attemptRequest.GetBody = nil
	attemptRequest.ContentLength = prepared.body.ContentLength()
	attemptRequest.TransferEncoding = nil
	response, roundTripErr := t.base.RoundTrip(attemptRequest)
	prepared.body.Retire()
	if response != nil {
		bindResponseHeaderPlan(response, attemptRequest, resolution.Plan.Headers.Response)
	}
	return response, roundTripErr
}

type mutatingBody struct {
	source      io.ReadCloser
	mutate      func([]byte) ([]byte, error)
	buffer      []byte
	pending     []byte
	terminalErr error
	closed      bool
}

func newMutatingBody(source io.ReadCloser, mutate func([]byte) ([]byte, error)) io.ReadCloser {
	return &mutatingBody{source: source, mutate: mutate, buffer: make([]byte, 64<<10)}
}

func (b *mutatingBody) Read(target []byte) (int, error) {
	if b == nil || b.source == nil || b.closed {
		return 0, io.EOF
	}
	if len(target) == 0 {
		return 0, nil
	}
	for len(b.pending) == 0 && b.terminalErr == nil {
		n, err := b.source.Read(b.buffer)
		if n > 0 {
			data := b.buffer[:n]
			if b.mutate != nil {
				data, b.terminalErr = b.mutate(data)
			}
			b.pending = data
		}
		if err != nil {
			b.terminalErr = err
		}
	}
	if len(b.pending) > 0 {
		n := copy(target, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}
	err := b.terminalErr
	if err == nil {
		err = io.EOF
	}
	_ = b.Close()
	return 0, err
}

func (b *mutatingBody) Close() error {
	if b == nil || b.closed {
		return nil
	}
	b.closed = true
	if b.source != nil {
		return b.source.Close()
	}
	return nil
}

func directiveErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidDirective):
		return "invalid_directive"
	case errors.Is(err, ErrDirectiveUnauthorized):
		return "directive_unauthorized"
	case errors.Is(err, ErrDirectiveTokenTooLarge):
		return "directive_token_too_large"
	case errors.Is(err, ErrDirectiveNotFound):
		return "directive_not_found"
	case errors.Is(err, ErrRemoteDirectiveUnavailable):
		return "remote_unavailable"
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
		Headers  httpheader.Plan
		Metadata map[string][]string
		Modules  []module.Spec
		Recovery any
	}{
		Target:   urlString(plan.Target),
		Proxy:    urlString(plan.Proxy),
		Headers:  plan.Headers,
		Metadata: plan.Metadata,
		Modules:  plan.Modules,
		Recovery: plan.Recovery,
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
