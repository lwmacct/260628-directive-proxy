package remotetoken

import (
	"errors"
	"net/url"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

func Generate(hmacSecret, resolverURL, resolverToken, remoteUUID string) (string, error) {
	hmacSecret = strings.TrimSpace(hmacSecret)
	resolverToken = strings.TrimSpace(resolverToken)
	if hmacSecret == "" || resolverToken == "" {
		return "", errors.New("HMAC secret and resolver token are required")
	}
	resolverURL = strings.TrimSpace(resolverURL)
	parsed, err := url.Parse(resolverURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return "", errors.New("invalid resolver URL")
	}
	spec := directive.RemoteSpec{
		HTTP: &directive.HTTPRemoteSpec{
			URL: parsed.String(),
			Headers: &directive.HeaderPolicy{
				Mutations: []directive.HeaderMutation{{
					Side:   directive.HeaderSideRequest,
					Action: directive.HeaderActionSet,
					Name:   "Authorization",
					Values: []string{"Bearer " + resolverToken},
				}},
			},
		},
	}
	spec.UUID = strings.TrimSpace(remoteUUID)
	return directive.EncodeRemote(hmacSecret, spec)
}
