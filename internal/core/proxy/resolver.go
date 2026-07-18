package proxy

import (
	"errors"
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
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
	Prepare(*http.Request) (*PreparedDirective, error)
}

// PreparedDirective is one immutable compilation result. Remote dereference,
// Payload validation and Program compilation are complete before this value is
// constructed; every RoundTrip consumes the same Plan and Recovery policy.
type PreparedDirective struct {
	source   DirectiveSource
	plan     *Plan
	program  *program.Executable
	recovery *recovery.Policy
	metadata metadata.Set
}

func NewPreparedDirective(source DirectiveSource, plan *Plan, executable *program.Executable, policy *recovery.Policy, fields metadata.Set) (*PreparedDirective, error) {
	if plan == nil || plan.Target == nil || fields.TraceID() != "" ||
		policy != nil && (policy.Controller.Binding == nil || policy.Controller.Spec.Module == "") {
		return nil, ErrInvalidDirective
	}
	cloned := ClonePlan(plan)
	return &PreparedDirective{
		source: source, plan: cloned, program: executable, recovery: recovery.ClonePolicy(policy), metadata: fields,
	}, nil
}

func (prepared *PreparedDirective) Source() DirectiveSource {
	if prepared == nil {
		return DirectiveSource{}
	}
	return prepared.source
}

func (prepared *PreparedDirective) Plan() *Plan {
	if prepared == nil {
		return nil
	}
	return ClonePlan(prepared.plan)
}

func (prepared *PreparedDirective) Program() *program.Executable {
	if prepared == nil {
		return nil
	}
	return prepared.program
}

func (prepared *PreparedDirective) Recovery() *recovery.Policy {
	if prepared == nil {
		return nil
	}
	return recovery.ClonePolicy(prepared.recovery)
}

func (prepared *PreparedDirective) Metadata() metadata.Set {
	if prepared == nil {
		return metadata.Set{}
	}
	return prepared.metadata
}
