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

func (r *Resolver) Resolve(req *http.Request) (proxy.Resolution, error) {
	raw, ok := directiveTokenFromAuthorization(req)
	if !ok {
		return proxy.Resolution{}, proxy.ErrNoMatch
	}
	if r != nil && r.maxTokenBytes > 0 && int64(len(raw)) > r.maxTokenBytes {
		return proxy.Resolution{}, proxy.ErrDirectiveTokenTooLarge
	}
	var maxInlineBytes int64
	if r != nil {
		maxInlineBytes = r.maxInlineBytes
	}
	document, err := DecodeWithOptions(raw, DecodeOptions{MaxInlineBytes: maxInlineBytes})
	if errors.Is(err, ErrPayloadTooLarge) {
		return proxy.Resolution{}, proxy.ErrDirectiveTokenTooLarge
	}
	if err != nil {
		return proxy.Resolution{}, proxy.ErrInvalidDirective
	}

	startedAt := time.Now()
	payload := document.Payload
	if document.Kind == KindRemote {
		if r == nil || r.remoteReader == nil {
			return proxy.Resolution{}, proxy.ErrRemoteDirectiveUnavailable
		}
		ctx := req.Context()
		if r.lookupTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, r.lookupTimeout)
			defer cancel()
		}
		payloadRaw, err := r.remoteReader.Read(ctx, *document.Remote, req)
		switch {
		case errors.Is(err, ErrRemoteNotFound):
			slog.Warn("remote directive not found", "directive_backend", document.Remote.Type, "directive_key", document.Remote.Key)
			return proxy.Resolution{}, proxy.ErrDirectiveNotFound
		case errors.Is(err, ErrRemoteMetadataTooBig):
			return proxy.Resolution{}, proxy.ErrDirectiveMetadataTooLarge
		case errors.Is(err, ErrRemoteInvalid):
			return proxy.Resolution{}, proxy.ErrRemoteDirectiveInvalid
		case err != nil:
			slog.Warn("resolve remote directive", "directive_backend", document.Remote.Type, "directive_key", document.Remote.Key, "error", err)
			return proxy.Resolution{}, proxy.ErrRemoteDirectiveUnavailable
		}
		decoded, decodeErr := DecodePayload(payloadRaw)
		if decodeErr != nil {
			slog.Error("decode remote directive", "directive_backend", document.Remote.Type, "directive_key", document.Remote.Key, "error", decodeErr)
			return proxy.Resolution{}, proxy.ErrRemoteDirectiveInvalid
		}
		payload = &decoded
	}
	plan, err := ToPlan(*payload, AssembleOptions{StripHeaders: []string{"Authorization"}})
	if err != nil {
		if document.Kind == KindRemote {
			slog.Error("compile remote directive", "directive_backend", document.Remote.Type, "directive_key", document.Remote.Key, "error", err)
			return proxy.Resolution{}, proxy.ErrRemoteDirectiveInvalid
		}
		return proxy.Resolution{}, proxy.ErrInvalidDirective
	}
	resolution := proxy.Resolution{Plan: plan, Source: proxy.SourceMetadata{Mode: KindInline}}
	if document.Kind == KindRemote {
		resolution.Source = proxy.SourceMetadata{
			Mode:     KindRemote,
			Backend:  document.Remote.Type,
			Endpoint: sanitizeRemoteEndpoint(document.Remote.URL),
			Key:      document.Remote.Key,
			Duration: time.Since(startedAt),
		}
	}
	return resolution, nil
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
// dproxy family. It does not decode or validate the token.
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
