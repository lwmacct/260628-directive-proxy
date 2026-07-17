package directive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

var (
	ErrRemoteNotFound       = errors.New("remote directive not found")
	ErrRemoteUnavailable    = errors.New("remote directive unavailable")
	ErrRemoteInvalid        = errors.New("remote directive response is invalid")
	ErrRemoteMetadataTooBig = errors.New("remote directive request metadata too large")
)

type RemoteReader interface {
	Read(context.Context, RemoteSpec, *http.Request) ([]byte, error)
}

type ResolverOptions struct {
	RemoteReader   RemoteReader
	LookupTimeout  time.Duration
	MaxTokenBytes  int64
	MaxInlineBytes int64
}

type Resolver struct {
	remoteReader   RemoteReader
	lookupTimeout  time.Duration
	maxTokenBytes  int64
	maxInlineBytes int64
}

type inlinePrepared struct {
	plan           *proxy.Plan
	requestProgram []module.Spec
	recovery       *recovery.Policy
}

type remotePrepared struct {
	reader         RemoteReader
	lookupTimeout  time.Duration
	spec           RemoteSpec
	requestProgram []module.Spec
	request        *http.Request
	recovery       *recovery.Policy
}

func NewResolver(opts ...ResolverOptions) proxy.Resolver {
	var configured ResolverOptions
	if len(opts) > 0 {
		configured = opts[0]
	}
	return &Resolver{
		remoteReader:   configured.RemoteReader,
		lookupTimeout:  configured.LookupTimeout,
		maxTokenBytes:  configured.MaxTokenBytes,
		maxInlineBytes: configured.MaxInlineBytes,
	}
}

// Prepare validates the stable token envelope exactly once. A remote payload
// is intentionally not read here: every attempt must observe the current
// remote directive and must never fall back to an earlier plan.
func (r *Resolver) Prepare(req *http.Request) (proxy.PreparedDirective, error) {
	raw, ok := directiveTokenFromAuthorization(req)
	if !ok {
		return nil, proxy.ErrNoMatch
	}
	if r != nil && r.maxTokenBytes > 0 && int64(len(raw)) > r.maxTokenBytes {
		return nil, proxy.ErrDirectiveTokenTooLarge
	}
	var maxInlineBytes int64
	if r != nil {
		maxInlineBytes = r.maxInlineBytes
	}
	document, err := DecodeWithOptions(raw, DecodeOptions{MaxInlineBytes: maxInlineBytes})
	if errors.Is(err, ErrPayloadTooLarge) {
		return nil, proxy.ErrDirectiveTokenTooLarge
	}
	if err != nil {
		return nil, proxy.ErrInvalidDirective
	}
	recoveryPolicy, err := CompileRecovery(document.Recovery)
	if err != nil {
		return nil, proxy.ErrInvalidDirective
	}

	if document.Kind == KindRemote {
		if r == nil || r.remoteReader == nil {
			return nil, proxy.ErrRemoteDirectiveUnavailable
		}
		return &remotePrepared{
			reader:         r.remoteReader,
			lookupTimeout:  r.lookupTimeout,
			spec:           cloneRemoteSpec(document.Remote.Source),
			requestProgram: cloneModuleSpecs(document.Remote.Program.Request),
			request:        snapshotResolveRequest(req),
			recovery:       recovery.ClonePolicy(recoveryPolicy),
		}, nil
	}

	plan, err := ToPlan(*document.Payload, AssembleOptions{StripHeaders: []string{"Authorization"}})
	if err != nil {
		return nil, proxy.ErrInvalidDirective
	}
	plan.Recovery = recovery.ClonePolicy(recoveryPolicy)
	return &inlinePrepared{
		plan: proxy.ClonePlan(plan), requestProgram: cloneModuleSpecs(document.Payload.Program.Request), recovery: recoveryPolicy,
	}, nil
}

func (*inlinePrepared) Kind() string { return KindInline }

func (p *inlinePrepared) RequestProgram() []module.Spec {
	if p == nil {
		return nil
	}
	return cloneModuleSpecs(p.requestProgram)
}

func (*inlinePrepared) Source() proxy.SourceMetadata {
	return proxy.SourceMetadata{Mode: KindInline}
}

func (p *inlinePrepared) Recovery() *recovery.Policy {
	if p == nil {
		return nil
	}
	return recovery.ClonePolicy(p.recovery)
}

func (p *inlinePrepared) ResolveAttempt(context.Context, int) (proxy.Resolution, error) {
	if p == nil || p.plan == nil {
		return proxy.Resolution{}, proxy.ErrInvalidDirective
	}
	return proxy.Resolution{
		Plan:   proxy.ClonePlan(p.plan),
		Source: proxy.SourceMetadata{Mode: KindInline},
	}, nil
}

func (*remotePrepared) Kind() string { return KindRemote }

func (p *remotePrepared) RequestProgram() []module.Spec {
	if p == nil {
		return nil
	}
	return cloneModuleSpecs(p.requestProgram)
}

func (p *remotePrepared) Source() proxy.SourceMetadata {
	if p == nil {
		return proxy.SourceMetadata{Mode: KindRemote}
	}
	return proxy.SourceMetadata{
		Mode:     KindRemote,
		Backend:  p.spec.Type,
		Endpoint: sanitizeRemoteEndpoint(p.spec.URL),
		Key:      p.spec.Key,
	}
}

func (p *remotePrepared) Recovery() *recovery.Policy {
	if p == nil {
		return nil
	}
	return recovery.ClonePolicy(p.recovery)
}

func (p *remotePrepared) ResolveAttempt(ctx context.Context, _ int) (proxy.Resolution, error) {
	if p == nil || p.reader == nil || p.request == nil {
		return proxy.Resolution{}, proxy.ErrRemoteDirectiveUnavailable
	}
	startedAt := time.Now()
	if p.lookupTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.lookupTimeout)
		defer cancel()
	}
	resolveRequest := p.request.Clone(ctx)
	payloadRaw, err := p.reader.Read(ctx, cloneRemoteSpec(p.spec), resolveRequest)
	switch {
	case errors.Is(err, ErrRemoteNotFound):
		slog.Warn("remote directive not found", "directive_backend", p.spec.Type, "directive_key", p.spec.Key)
		return proxy.Resolution{}, proxy.ErrDirectiveNotFound
	case errors.Is(err, ErrRemoteMetadataTooBig):
		return proxy.Resolution{}, proxy.ErrDirectiveMetadataTooLarge
	case errors.Is(err, ErrRemoteInvalid):
		return proxy.Resolution{}, proxy.ErrRemoteDirectiveInvalid
	case err != nil:
		slog.Warn("resolve remote directive", "directive_backend", p.spec.Type, "directive_key", p.spec.Key, "error", err)
		return proxy.Resolution{}, proxy.ErrRemoteDirectiveUnavailable
	}
	decoded, decodeErr := DecodePayload(payloadRaw)
	if decodeErr != nil {
		slog.Error("decode remote directive", "directive_backend", p.spec.Type, "directive_key", p.spec.Key, "error", decodeErr)
		return proxy.Resolution{}, proxy.ErrRemoteDirectiveInvalid
	}
	if len(decoded.Program.Request) > 0 {
		return proxy.Resolution{}, proxy.ErrRemoteDirectiveInvalid
	}
	plan, err := ToPlan(decoded, AssembleOptions{StripHeaders: []string{"Authorization"}})
	if err != nil {
		slog.Error("compile remote directive", "directive_backend", p.spec.Type, "directive_key", p.spec.Key, "error", err)
		return proxy.Resolution{}, proxy.ErrRemoteDirectiveInvalid
	}
	plan.Recovery = recovery.ClonePolicy(p.recovery)
	digest := sha256.Sum256(payloadRaw)
	return proxy.Resolution{
		Plan: plan,
		Source: proxy.SourceMetadata{
			Mode:          KindRemote,
			Backend:       p.spec.Type,
			Endpoint:      sanitizeRemoteEndpoint(p.spec.URL),
			Key:           p.spec.Key,
			Duration:      time.Since(startedAt),
			PayloadSHA256: hex.EncodeToString(digest[:]),
		},
	}, nil
}

func cloneModuleSpecs(in []module.Spec) []module.Spec {
	out := make([]module.Spec, len(in))
	for index, spec := range in {
		out[index] = spec
		out[index].Config = append([]byte(nil), spec.Config...)
	}
	return out
}

func snapshotResolveRequest(req *http.Request) *http.Request {
	if req == nil {
		return nil
	}
	snapshot := req.Clone(context.Background())
	snapshot.Body = http.NoBody
	snapshot.GetBody = nil
	snapshot.ContentLength = 0
	return snapshot
}

func cloneRemoteSpec(in RemoteSpec) RemoteSpec {
	out := in
	out.RequestHeaders = append([]string(nil), in.RequestHeaders...)
	if in.Headers != nil {
		out.Headers = make(map[string]string, len(in.Headers))
		for name, value := range in.Headers {
			out.Headers[name] = value
		}
	}
	return out
}

func sanitizeRemoteEndpoint(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// MatchesRequest reports whether the request carries a token from the reserved
// dp family. It does not decode or validate the token.
func MatchesRequest(req *http.Request) bool {
	_, ok := directiveTokenFromAuthorization(req)
	return ok
}

func directiveTokenFromAuthorization(req *http.Request) (string, bool) {
	if req == nil {
		return "", false
	}
	header := strings.TrimSpace(req.Header.Get("Authorization"))
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	raw := parts[1]
	if !strings.HasPrefix(raw, TokenFamily+".") {
		return "", false
	}
	return raw, true
}
