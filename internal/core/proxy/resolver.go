package proxy

import (
	"errors"
	"net/http"
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
	Resolve(*http.Request) (Resolution, error)
}
