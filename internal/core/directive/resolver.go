package directive

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
)

var ErrStoreKeyNotFound = errors.New("directive store key not found")

type Store interface {
	Get(context.Context, string) ([]byte, error)
}

type ResolverOptions struct {
	Store         Store
	LookupTimeout time.Duration
	MaxValueBytes int64
}

type Resolver struct {
	store         Store
	lookupTimeout time.Duration
	maxValueBytes int64
}

func NewResolver(opts ...ResolverOptions) proxy.Resolver {
	var configured ResolverOptions
	if len(opts) > 0 {
		configured = opts[0]
	}
	return &Resolver{
		store:         configured.Store,
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
	if token.Kind == TokenRedis {
		if r == nil || r.store == nil {
			return nil, proxy.ErrDirectiveStoreUnavailable
		}
		ctx := req.Context()
		if r.lookupTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, r.lookupTimeout)
			defer cancel()
		}
		payloadRaw, err = r.store.Get(ctx, token.RedisKey)
		if errors.Is(err, ErrStoreKeyNotFound) {
			slog.Warn("redis directive not found", "directive_key", token.RedisKey)
			return nil, proxy.ErrDirectiveNotFound
		}
		if err != nil {
			slog.Warn("read redis directive", "directive_key", token.RedisKey, "error", err)
			return nil, proxy.ErrDirectiveStoreUnavailable
		}
		if r.maxValueBytes > 0 && int64(len(payloadRaw)) > r.maxValueBytes {
			slog.Error("redis directive exceeds value limit", "directive_key", token.RedisKey, "bytes", len(payloadRaw), "limit", r.maxValueBytes)
			return nil, proxy.ErrStoredDirectiveInvalid
		}
	}
	payload, err := DecodePayload(payloadRaw)
	if err != nil {
		if token.Kind == TokenRedis {
			slog.Error("decode redis directive", "directive_key", token.RedisKey, "error", err)
			return nil, proxy.ErrStoredDirectiveInvalid
		}
		return nil, proxy.ErrInvalidDirective
	}
	plan, err := ToPlan(payload, AssembleOptions{
		StripHeaders: []string{"Authorization"},
	})
	if err != nil {
		if token.Kind == TokenRedis {
			slog.Error("compile redis directive", "directive_key", token.RedisKey, "error", err)
			return nil, proxy.ErrStoredDirectiveInvalid
		}
		return nil, proxy.ErrInvalidDirective
	}
	plan.DirectiveSource = "inline"
	if token.Kind == TokenRedis {
		plan.DirectiveSource = "redis"
		plan.DirectiveKey = token.RedisKey
		plan.DirectiveLookupMillis = time.Since(startedAt).Milliseconds()
	}
	return plan, nil
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
