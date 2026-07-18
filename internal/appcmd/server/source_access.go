package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/lwmacct/260718-go-pkg-clientip/pkg/clientip"
	"github.com/lwmacct/260718-go-pkg-ipallow/pkg/ipallow"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
)

const (
	sourceInvalidCode    = "source_invalid"
	sourceNotAllowedCode = "source_not_allowed"
	sourceEngineClosed   = "engine_closed"
)

type directiveSourceAccess struct {
	extractor *clientip.Extractor
	matcher   *ipallow.Matcher
	policy    *ipallow.Policy
}

func newDirectiveSourceAccess(ctx context.Context, cfg config.DirectiveSourceAccess) (*directiveSourceAccess, error) {
	policy, err := ipallow.Compile(cfg.Rules)
	if err != nil || policy.Len() == 0 {
		return nil, config.ErrInvalidAccess
	}
	matcher, err := ipallow.NewMatcher(ctx, cfg.DNS)
	if err != nil {
		return nil, err
	}
	extractor, err := clientip.New(cfg.SourceClientIPConfig)
	if err != nil {
		matcher.Close()
		return nil, err
	}
	return &directiveSourceAccess{extractor: extractor, matcher: matcher, policy: policy}, nil
}

func (a *directiveSourceAccess) RequireAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if a == nil || a.extractor == nil || a.matcher == nil || a.policy == nil {
			writeSourceAccessDenied(w, sourceNotAllowedCode)
			return
		}
		info, err := a.extractor.Extract(request)
		if err != nil {
			writeSourceAccessDenied(w, sourceInvalidCode)
			return
		}
		result, err := a.matcher.Match(request.Context(), a.policy, info.Addr)
		if err != nil || !result.Allowed {
			code := sourceNotAllowedCode
			if errors.Is(err, ipallow.ErrMatcherClosed) {
				code = sourceEngineClosed
			}
			writeSourceAccessDenied(w, code)
			return
		}
		next.ServeHTTP(w, request)
	})
}

func (a *directiveSourceAccess) Close() {
	if a != nil && a.matcher != nil {
		a.matcher.Close()
	}
}

func writeSourceAccessDenied(w http.ResponseWriter, code string) {
	proxy.WriteProxyErrorJSON(w, http.StatusForbidden, code, "directive: source access denied")
}
