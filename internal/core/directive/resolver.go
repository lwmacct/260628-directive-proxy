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

type preparedDirective struct {
	kind           string
	plan           *proxy.Plan
	requestProgram []module.Spec
	source         proxy.SourceMetadata
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

// Prepare resolves a token to one canonical Payload. Inline tokens contain the
// Payload directly; remote tokens contain only the RemoteSpec used to fetch it.
// After this dereference both modes share the same validation and execution path.
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

	payload := document.Payload
	source := proxy.SourceMetadata{Mode: KindInline}
	if document.Kind == KindRemote {
		if r == nil || r.remoteReader == nil {
			return nil, proxy.ErrRemoteDirectiveUnavailable
		}
		resolved, metadata, resolveErr := r.resolveRemotePayload(req, *document.Remote)
		if resolveErr != nil {
			return nil, resolveErr
		}
		payload = &resolved
		source = metadata
	}
	if payload == nil {
		return nil, proxy.ErrInvalidDirective
	}
	plan, err := ToPlan(*payload, AssembleOptions{StripHeaders: []string{"Authorization"}})
	if err != nil {
		if document.Kind == KindRemote {
			return nil, proxy.ErrRemoteDirectiveInvalid
		}
		return nil, proxy.ErrInvalidDirective
	}
	return &preparedDirective{
		kind: document.Kind, plan: proxy.ClonePlan(plan), requestProgram: cloneModuleSpecs(payload.Program.Request), source: source,
	}, nil
}

func (r *Resolver) resolveRemotePayload(req *http.Request, spec RemoteSpec) (Payload, proxy.SourceMetadata, error) {
	ctx := req.Context()
	if r.lookupTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.lookupTimeout)
		defer cancel()
	}
	startedAt := time.Now()
	resolveRequest := snapshotResolveRequest(req).Clone(ctx)
	payloadRaw, err := r.remoteReader.Read(ctx, cloneRemoteSpec(spec), resolveRequest)
	source := proxy.SourceMetadata{
		Mode: KindRemote, Backend: spec.Type, Endpoint: sanitizeRemoteEndpoint(spec.URL), Key: spec.Key, Duration: time.Since(startedAt),
	}
	switch {
	case errors.Is(err, ErrRemoteNotFound):
		slog.Warn("remote directive not found", "directive_backend", spec.Type, "directive_key", spec.Key)
		return Payload{}, source, proxy.ErrDirectiveNotFound
	case errors.Is(err, ErrRemoteMetadataTooBig):
		return Payload{}, source, proxy.ErrDirectiveMetadataTooLarge
	case errors.Is(err, ErrRemoteInvalid):
		return Payload{}, source, proxy.ErrRemoteDirectiveInvalid
	case err != nil:
		slog.Warn("resolve remote directive", "directive_backend", spec.Type, "directive_key", spec.Key, "error", err)
		return Payload{}, source, proxy.ErrRemoteDirectiveUnavailable
	}
	payload, decodeErr := DecodePayload(payloadRaw)
	if decodeErr != nil {
		slog.Error("decode remote directive", "directive_backend", spec.Type, "directive_key", spec.Key, "error", decodeErr)
		return Payload{}, source, proxy.ErrRemoteDirectiveInvalid
	}
	digest := sha256.Sum256(payloadRaw)
	source.PayloadSHA256 = hex.EncodeToString(digest[:])
	return payload, source, nil
}

func (p *preparedDirective) Kind() string {
	if p == nil {
		return ""
	}
	return p.kind
}

func (p *preparedDirective) RequestProgram() []module.Spec {
	if p == nil {
		return nil
	}
	return cloneModuleSpecs(p.requestProgram)
}

func (p *preparedDirective) Source() proxy.SourceMetadata {
	if p == nil {
		return proxy.SourceMetadata{}
	}
	return p.source
}

func (p *preparedDirective) Recovery() *recovery.Policy {
	if p == nil || p.plan == nil {
		return nil
	}
	return recovery.ClonePolicy(p.plan.Recovery)
}

func (p *preparedDirective) ResolveAttempt(context.Context, int) (proxy.Resolution, error) {
	if p == nil || p.plan == nil {
		return proxy.Resolution{}, proxy.ErrInvalidDirective
	}
	return proxy.Resolution{Plan: proxy.ClonePlan(p.plan), Source: p.source}, nil
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
	if in.Headers != nil {
		headers := *in.Headers
		headers.Mutations = append([]HeaderMutation(nil), in.Headers.Mutations...)
		for index := range headers.Mutations {
			headers.Mutations[index].Values = append([]string(nil), headers.Mutations[index].Values...)
		}
		out.Headers = &headers
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
