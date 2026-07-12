package directive

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
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
	RemoteReader  RemoteReader
	LookupTimeout time.Duration
	MaxValueBytes int64
}

type Resolver struct {
	remoteReader  RemoteReader
	lookupTimeout time.Duration
	maxValueBytes int64
}

func NewResolver(opts ...ResolverOptions) proxy.Resolver {
	var configured ResolverOptions
	if len(opts) > 0 {
		configured = opts[0]
	}
	return &Resolver{
		remoteReader:  configured.RemoteReader,
		lookupTimeout: configured.LookupTimeout,
		maxValueBytes: configured.MaxValueBytes,
	}
}

func (*Resolver) Match(req *http.Request) bool {
	_, ok := directiveTokenFromAuthorization(req)
	return ok
}

func (r *Resolver) Resolve(req *http.Request) (*proxy.Plan, error) {
	raw, ok := directiveTokenFromAuthorization(req)
	if !ok {
		return nil, proxy.ErrNoMatch
	}
	token, err := Decode(raw)
	if err != nil {
		return nil, proxy.ErrInvalidDirective
	}

	startedAt := time.Now()
	payloadRaw := token.Payload
	if token.Kind == TokenRemote {
		if r == nil || r.remoteReader == nil {
			return nil, proxy.ErrRemoteDirectiveUnavailable
		}
		ctx := req.Context()
		if r.lookupTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, r.lookupTimeout)
			defer cancel()
		}
		payloadRaw, err = r.remoteReader.Read(ctx, token.Remote, req)
		switch {
		case errors.Is(err, ErrRemoteNotFound):
			slog.Warn("remote directive not found", "directive_backend", token.Remote.Type, "directive_key", token.Remote.Key)
			return nil, proxy.ErrDirectiveNotFound
		case errors.Is(err, ErrRemoteMetadataTooBig):
			return nil, proxy.ErrDirectiveMetadataTooLarge
		case errors.Is(err, ErrRemoteInvalid):
			return nil, proxy.ErrRemoteDirectiveInvalid
		case err != nil:
			slog.Warn("resolve remote directive", "directive_backend", token.Remote.Type, "directive_key", token.Remote.Key, "error", err)
			return nil, proxy.ErrRemoteDirectiveUnavailable
		}
		if r.maxValueBytes > 0 && int64(len(payloadRaw)) > r.maxValueBytes {
			slog.Error("remote directive exceeds value limit", "directive_backend", token.Remote.Type, "directive_key", token.Remote.Key, "bytes", len(payloadRaw), "limit", r.maxValueBytes)
			return nil, proxy.ErrRemoteDirectiveInvalid
		}
	}

	payload, err := DecodePayload(payloadRaw)
	if err != nil {
		if token.Kind == TokenRemote {
			slog.Error("decode remote directive", "directive_backend", token.Remote.Type, "directive_key", token.Remote.Key, "error", err)
			return nil, proxy.ErrRemoteDirectiveInvalid
		}
		return nil, proxy.ErrInvalidDirective
	}
	plan, err := ToPlan(payload, AssembleOptions{StripHeaders: []string{"Authorization"}})
	if err != nil {
		if token.Kind == TokenRemote {
			slog.Error("compile remote directive", "directive_backend", token.Remote.Type, "directive_key", token.Remote.Key, "error", err)
			return nil, proxy.ErrRemoteDirectiveInvalid
		}
		return nil, proxy.ErrInvalidDirective
	}
	plan.DirectiveMode = "inline"
	if token.Kind == TokenRemote {
		plan.DirectiveMode = "remote"
		plan.DirectiveBackend = token.Remote.Type
		plan.DirectiveEndpoint = sanitizeRemoteEndpoint(token.Remote.URL)
		plan.DirectiveKey = token.Remote.Key
		plan.DirectiveResolutionMillis = time.Since(startedAt).Milliseconds()
	}
	return plan, nil
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
