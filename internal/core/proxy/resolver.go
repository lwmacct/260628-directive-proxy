package proxy

import (
	"errors"
	"net/http"
)

var (
	ErrInvalidPlan      = errors.New("invalid proxy plan")
	ErrInvalidDirective = errors.New("invalid proxy directive")
)

type Resolver interface {
	Resolve(*http.Request) (*Plan, error)
}
