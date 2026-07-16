package proxy

import (
	"context"
	"errors"
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

var (
	ErrNoMatch                    = errors.New("proxy resolver did not match request")
	ErrInvalidDirective           = errors.New("invalid proxy directive")
	ErrDirectiveNotFound          = errors.New("directive reference not found")
	ErrRemoteDirectiveUnavailable = errors.New("remote directive unavailable")
	ErrDirectiveMetadataTooLarge  = errors.New("directive request metadata too large")
	ErrDirectiveTokenTooLarge     = errors.New("directive token is too large")
	ErrRemoteDirectiveInvalid     = errors.New("remote directive is invalid")
)

type Resolver interface {
	Prepare(*http.Request) (PreparedDirective, error)
}

// PreparedDirective is an immutable token envelope. Inline implementations
// return the same compiled plan for every attempt; remote implementations must
// fetch and compile a fresh payload for every ResolveAttempt call.
type PreparedDirective interface {
	Kind() string
	Source() SourceMetadata
	RequestProgram() []module.Spec
	ResolveAttempt(context.Context, int) (Resolution, error)
}
