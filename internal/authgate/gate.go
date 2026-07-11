package authgate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
)

const (
	flowLifetime  = 5 * time.Minute
	identityScope = "federated:id"
)

type Identity struct {
	GitHubID string `json:"github_id"`
	Username string `json:"username"`
	Avatar   string `json:"avatar_url"`
}

type flow struct {
	nonce    string
	verifier string
	expires  time.Time
}

type Gate struct {
	oauth        oauth2.Config
	verifier     *oidc.IDTokenVerifier
	publicURL    *url.URL
	maxAge       time.Duration
	secure       bool
	cookieName   string
	flowCookie   string
	allowedUsers map[string]struct{}
	mu           sync.Mutex
	flows        map[string]flow
}

type tokenClaims struct {
	IssuedAt          int64  `json:"iat"`
	PreferredUsername string `json:"preferred_username"`
	FederatedClaims   struct {
		ConnectorID string `json:"connector_id"`
		UserID      string `json:"user_id"`
	} `json:"federated_claims"`
}

func New(ctx context.Context, cfg config.ServerHTTPAuth) (*Gate, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}
	callback, err := url.Parse(cfg.CallbackURL)
	if err != nil {
		return nil, fmt.Errorf("parse oidc callback: %w", err)
	}
	publicURL, err := url.Parse(cfg.PublicURL)
	if err != nil {
		return nil, fmt.Errorf("parse public URL: %w", err)
	}
	endpoint := provider.Endpoint()
	// Public clients authenticate with client_id plus PKCE and have no secret.
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	gate := &Gate{
		oauth: oauth2.Config{
			ClientID:    cfg.ClientID,
			Endpoint:    endpoint,
			RedirectURL: cfg.CallbackURL,
			Scopes:      []string{oidc.ScopeOpenID, "profile", identityScope},
		},
		verifier:     provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		publicURL:    publicURL,
		maxAge:       cfg.SessionTTL,
		secure:       callback.Scheme == "https",
		cookieName:   "dproxy_identity_dev",
		flowCookie:   "dproxy_oidc_flow_dev",
		allowedUsers: make(map[string]struct{}),
		flows:        make(map[string]flow),
	}
	if gate.secure {
		gate.cookieName = "__Host-dproxy_identity"
		gate.flowCookie = "__Host-dproxy_oidc_flow"
	}
	for _, username := range cfg.AllowedUsers {
		gate.allowedUsers[username] = struct{}{}
	}
	return gate, nil
}

func (g *Gate) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", g.login)
	mux.HandleFunc("GET /auth/callback", g.callbackHandler)
	mux.HandleFunc("GET /auth/session", g.session)
	mux.HandleFunc("POST /auth/logout", g.logout)
	return mux
}

func (g *Gate) RequireAdministrator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := g.identity(r); err != nil {
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if isUnsafeMethod(r.Method) && !g.sameOrigin(r.Header.Get("Origin")) {
			writeJSONError(w, http.StatusForbidden, "invalid request origin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gate) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	verifier, err := randomToken()
	if err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	g.mu.Lock()
	now := time.Now()
	for key, item := range g.flows {
		if now.After(item.expires) {
			delete(g.flows, key)
		}
	}
	g.flows[state] = flow{nonce: nonce, verifier: verifier, expires: now.Add(flowLifetime)}
	g.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     g.flowCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   int(flowLifetime.Seconds()),
		HttpOnly: true,
		Secure:   g.secure,
		SameSite: http.SameSiteLaxMode,
	})
	challenge := sha256.Sum256([]byte(verifier))
	location := g.oauth.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:])),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	http.Redirect(w, r, location, http.StatusFound)
}

func (g *Gate) callbackHandler(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	flowCookie, cookieErr := r.Cookie(g.flowCookie)
	g.clearFlowCookie(w)
	g.mu.Lock()
	pending, ok := g.flows[state]
	delete(g.flows, state)
	g.mu.Unlock()
	if cookieErr != nil || flowCookie.Value != state || !ok || time.Now().After(pending.expires) || r.URL.Query().Get("code") == "" {
		http.Error(w, "invalid or expired login", http.StatusBadRequest)
		return
	}
	token, err := g.oauth.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.SetAuthURLParam("code_verifier", pending.verifier))
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) {
			slog.Warn("OIDC code exchange failed", "status", retrieveErr.Response.StatusCode, "oauth_error", retrieveErr.ErrorCode, "description", retrieveErr.ErrorDescription)
		} else {
			slog.Warn("OIDC code exchange failed", "error", err)
		}
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "identity token missing", http.StatusBadGateway)
		return
	}
	idToken, identity, claims, err := g.verify(r.Context(), rawIDToken)
	if err != nil || idToken.Nonce != pending.nonce {
		slog.Warn("OIDC identity validation failed", "error", err)
		http.Error(w, "invalid identity", http.StatusUnauthorized)
		return
	}
	if !g.allowed(identity) {
		slog.Warn("GitHub user is not an administrator", "github_id", identity.GitHubID, "username", identity.Username)
		http.Error(w, "GitHub user is not an administrator", http.StatusForbidden)
		return
	}
	maxAge := g.cookieMaxAge(idToken.Expiry, time.Unix(claims.IssuedAt, 0))
	if maxAge <= 0 {
		http.Error(w, "identity token expired", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: g.cookieName, Value: rawIDToken, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: g.secure, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, g.publicURL.String()+"/#/console", http.StatusFound)
}

func (g *Gate) clearFlowCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     g.flowCookie,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   g.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (g *Gate) session(w http.ResponseWriter, r *http.Request) {
	identity, err := g.identity(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(identity)
}

func (g *Gate) logout(w http.ResponseWriter, r *http.Request) {
	if !g.sameOrigin(r.Header.Get("Origin")) {
		writeJSONError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: g.cookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: g.secure, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gate) identity(r *http.Request) (Identity, error) {
	cookie, err := r.Cookie(g.cookieName)
	if err != nil {
		return Identity{}, err
	}
	idToken, identity, claims, err := g.verify(r.Context(), cookie.Value)
	if err != nil || !g.allowed(identity) {
		return Identity{}, errors.New("invalid identity")
	}
	if g.cookieMaxAge(idToken.Expiry, time.Unix(claims.IssuedAt, 0)) <= 0 {
		return Identity{}, errors.New("identity expired")
	}
	return identity, nil
}

func (g *Gate) verify(ctx context.Context, raw string) (*oidc.IDToken, Identity, tokenClaims, error) {
	idToken, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, Identity{}, tokenClaims{}, err
	}
	var claims tokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, Identity{}, claims, err
	}
	if claims.FederatedClaims.ConnectorID != "github" || claims.FederatedClaims.UserID == "" || claims.PreferredUsername == "" || claims.IssuedAt == 0 {
		return nil, Identity{}, claims, errors.New("required GitHub identity claims missing")
	}
	identity := Identity{
		GitHubID: claims.FederatedClaims.UserID,
		Username: claims.PreferredUsername,
		Avatar:   "https://avatars.githubusercontent.com/u/" + url.PathEscape(claims.FederatedClaims.UserID) + "?v=4",
	}
	return idToken, identity, claims, nil
}

func (g *Gate) allowed(identity Identity) bool {
	_, ok := g.allowedUsers[strings.ToLower(identity.Username)]
	return ok
}

func (g *Gate) cookieMaxAge(tokenExpiry, issuedAt time.Time) int {
	expires := issuedAt.Add(g.maxAge)
	if tokenExpiry.Before(expires) {
		expires = tokenExpiry
	}
	return int(time.Until(expires).Seconds())
}

func (g *Gate) sameOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == g.publicURL.Scheme && parsed.Host == g.publicURL.Host && parsed.Path == ""
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
