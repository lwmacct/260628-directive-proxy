package proxy

import (
	"errors"
	"net/http"
)

var (
	ErrNoMatch          = errors.New("proxy resolver did not match request")
	ErrInvalidDirective = errors.New("invalid proxy directive")
)

type Resolver interface {
	Resolve(*http.Request) (*Plan, error)
}
