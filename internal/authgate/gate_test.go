package authgate

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestLoginUsesStateNonceAndS256PKCE(t *testing.T) {
	gate := &Gate{
		oauth: oauth2.Config{
			ClientID:    "dproxy-local",
			RedirectURL: "http://localhost:23198/auth/callback",
			Endpoint:    oauth2.Endpoint{AuthURL: "https://auth.example.com/auth"},
			Scopes:      []string{"openid", "profile", identityScope},
		},
		flows:      make(map[string]flow),
		flowCookie: "oidc_flow",
	}
	recorder := httptest.NewRecorder()
	gate.login(recorder, httptest.NewRequest(http.MethodGet, "http://localhost:23198/auth/login", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	query := location.Query()
	if query.Get("state") == "" || query.Get("nonce") == "" || query.Get("code_challenge") == "" {
		t.Fatalf("missing secure flow parameters: %s", location.RawQuery)
	}
	if query.Get("code_challenge_method") != "S256" || !strings.Contains(query.Get("scope"), identityScope) {
		t.Fatalf("unexpected OIDC parameters: %s", location.RawQuery)
	}
	if len(gate.flows) != 1 {
		t.Fatalf("expected one pending flow, got %d", len(gate.flows))
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "oidc_flow" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected flow cookie: %#v", cookies)
	}
	if cookies[0].Value != query.Get("state") {
		t.Fatal("flow cookie is not bound to OIDC state")
	}
}

func TestPublicClientSendsClientIDWithoutSecret(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("client_id") != "dproxy-local" || r.FormValue("client_secret") != "" {
			t.Errorf("unexpected client credentials: id=%q secret=%q", r.FormValue("client_id"), r.FormValue("client_secret"))
		}
		if _, _, ok := r.BasicAuth(); ok {
			t.Error("public client must not use HTTP Basic authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer"}`))
	}))
	defer tokenServer.Close()

	gate := &Gate{oauth: oauth2.Config{
		ClientID: "dproxy-local",
		Endpoint: oauth2.Endpoint{TokenURL: tokenServer.URL, AuthStyle: oauth2.AuthStyleInParams},
	}}
	if _, err := gate.oauth.Exchange(t.Context(), "code", oauth2.SetAuthURLParam("code_verifier", "verifier")); err != nil {
		t.Fatalf("exchange public client token: %v", err)
	}
}

func TestRequireAdministratorRejectsMissingIdentity(t *testing.T) {
	gate := &Gate{cookieName: "identity"}
	nextCalled := false
	handler := gate.RequireAdministrator(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://localhost/api/health", nil))

	if recorder.Code != http.StatusUnauthorized || nextCalled {
		t.Fatalf("unexpected auth result: status=%d next=%v", recorder.Code, nextCalled)
	}
}

func TestAdministratorMatchesUsernameOnly(t *testing.T) {
	gate := &Gate{allowedUsers: map[string]struct{}{"lwmacct": {}}}
	if !gate.allowed(Identity{GitHubID: "30756209", Username: "LwMacct"}) {
		t.Fatal("configured username must match case-insensitively")
	}
	if gate.allowed(Identity{GitHubID: "30756209", Username: "renamed"}) {
		t.Fatal("GitHub ID must not bypass the configured username")
	}
	if gate.allowed(Identity{GitHubID: "2", Username: "visitor"}) {
		t.Fatal("visitor was unexpectedly authorized")
	}
}

func TestSameOriginRequiresCallbackOrigin(t *testing.T) {
	publicURL, _ := url.Parse("https://tool.example.com")
	gate := &Gate{publicURL: publicURL}
	if !gate.sameOrigin("https://tool.example.com") {
		t.Fatal("expected callback origin to match")
	}
	for _, origin := range []string{"", "https://evil.example.com", "http://tool.example.com", "https://tool.example.com/path"} {
		if gate.sameOrigin(origin) {
			t.Fatalf("unexpected origin match: %q", origin)
		}
	}
}

func TestCookieMaxAgeUsesShortestLifetime(t *testing.T) {
	now := time.Now()
	gate := &Gate{maxAge: time.Hour}
	maxAge := gate.cookieMaxAge(now.Add(2*time.Hour), now)
	if maxAge < 3590 || maxAge > 3600 {
		t.Fatalf("unexpected max age: %d", maxAge)
	}
}
