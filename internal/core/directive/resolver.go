package directive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

var (
	ErrRemoteNotFound    = errors.New("remote directive not found")
	ErrRemoteUnavailable = errors.New("remote directive unavailable")
	ErrRemoteInvalid     = errors.New("remote directive response is invalid")
)

type ResolverOptions struct {
	HTTPReader    HTTPRemoteReader
	RedisReader   RedisRemoteReader
	FileReader    FileRemoteReader
	LookupTimeout time.Duration
	MaxTokenBytes int64
	TokenSecret   string
}

type Resolver struct {
	httpReader    HTTPRemoteReader
	redisReader   RedisRemoteReader
	fileReader    FileRemoteReader
	lookupTimeout time.Duration
	maxTokenBytes int64
	tokenSecret   string
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
		httpReader:    configured.HTTPReader,
		redisReader:   configured.RedisReader,
		fileReader:    configured.FileReader,
		lookupTimeout: configured.LookupTimeout,
		maxTokenBytes: configured.MaxTokenBytes,
		tokenSecret:   configured.TokenSecret,
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
	var tokenSecret string
	if r != nil {
		tokenSecret = r.tokenSecret
	}
	document, err := Decode(tokenSecret, raw)
	if errors.Is(err, ErrTokenUnauthorized) {
		return nil, proxy.ErrDirectiveUnauthorized
	}
	if err != nil {
		return nil, proxy.ErrInvalidDirective
	}

	payload := document.Payload
	source := proxy.SourceMetadata{Mode: KindInline}
	if document.Kind == KindRemote {
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
	reference, err := compileRemoteSpec(spec)
	source := proxy.SourceMetadata{
		Mode: KindRemote, Backend: reference.backend, Endpoint: reference.endpoint, Resource: reference.resource,
	}
	if err != nil {
		return Payload{}, source, proxy.ErrInvalidDirective
	}
	ctx := req.Context()
	if r.lookupTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.lookupTimeout)
		defer cancel()
	}
	startedAt := time.Now()
	requestSnapshot := snapshotRequest(req)
	var payloadRaw []byte
	switch {
	case reference.http != nil:
		if r == nil || r.httpReader == nil {
			err = ErrRemoteUnavailable
			break
		}
		payloadRaw, err = r.httpReader.Read(ctx, *reference.http, requestSnapshot)
	case reference.redis != nil:
		if r == nil || r.redisReader == nil {
			err = ErrRemoteUnavailable
			break
		}
		payloadRaw, err = r.redisReader.Read(ctx, *reference.redis)
	case reference.file != nil:
		if r == nil || r.fileReader == nil {
			err = ErrRemoteUnavailable
			break
		}
		payloadRaw, err = r.fileReader.Read(ctx, *reference.file)
	default:
		err = ErrRemoteInvalid
	}
	source.Duration = time.Since(startedAt)
	switch {
	case errors.Is(err, ErrRemoteNotFound):
		slog.Warn("remote directive not found", "directive_backend", source.Backend, "directive_endpoint", source.Endpoint, "directive_resource", source.Resource, "error", err)
		return Payload{}, source, proxy.ErrDirectiveNotFound
	case errors.Is(err, ErrRemoteInvalid):
		slog.Warn("remote directive response invalid", "directive_backend", source.Backend, "directive_endpoint", source.Endpoint, "directive_resource", source.Resource, "error", err)
		return Payload{}, source, proxy.ErrRemoteDirectiveInvalid
	case err != nil:
		slog.Warn("resolve remote directive", "directive_backend", source.Backend, "directive_endpoint", source.Endpoint, "directive_resource", source.Resource, "error", err)
		return Payload{}, source, proxy.ErrRemoteDirectiveUnavailable
	}
	payload, decodeErr := DecodePayload(payloadRaw)
	if decodeErr != nil {
		slog.Error("decode remote directive", "directive_backend", source.Backend, "directive_endpoint", source.Endpoint, "directive_resource", source.Resource, "error", decodeErr)
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
