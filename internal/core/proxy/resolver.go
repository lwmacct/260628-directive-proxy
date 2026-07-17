package proxy

import (
	"context"
	"errors"
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

var (
	ErrNoMatch                    = errors.New("proxy resolver did not match request")
	ErrInvalidDirective           = errors.New("invalid proxy directive")
	ErrDirectiveUnauthorized      = errors.New("directive token unauthorized")
	ErrDirectiveNotFound          = errors.New("directive reference not found")
	ErrRemoteDirectiveUnavailable = errors.New("remote directive unavailable")
	ErrDirectiveTokenTooLarge     = errors.New("directive token is too large")
	ErrRemoteDirectiveInvalid     = errors.New("remote directive is invalid")
)

type Resolver interface {
	Prepare(*http.Request) (PreparedDirective, error)
}

// PreparedDirective is an immutable, fully resolved Payload. A remote token is
// dereferenced before this boundary, so attempts do not distinguish how the
// Payload was acquired.
type PreparedDirective interface {
	Kind() string
	Source() SourceMetadata
	RequestProgram() []module.Spec
	ResolveAttempt(context.Context, int) (Resolution, error)
}

type RecoveryPreparedDirective interface {
	Recovery() *recovery.Policy
}

func PreparedRecovery(prepared PreparedDirective) *recovery.Policy {
	value, ok := prepared.(RecoveryPreparedDirective)
	if !ok || value == nil {
		return nil
	}
	return value.Recovery()
}
