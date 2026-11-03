package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const sessionCookie = "vrac_session"

type authOptions struct {
	Token               string
	Dev                 bool
	OAuthIssuer         string
	OAuthAudience       string
	OAuthScope          string
	WebOIDCClientID     string
	WebOIDCProviderName string
}

type auth struct {
	options  authOptions
	token    [32]byte
	verifier *oidc.IDTokenVerifier
	browser  *browserAuth
	flows    map[[32]byte]browserFlow
	mu       sync.Mutex
	sessions map[[32]byte]time.Time
	failures int
	reset    time.Time
}

func newAuth(ctx context.Context, opts authOptions) (*auth, error) {
	if !opts.Dev && len(opts.Token) < 32 {
		return nil, errors.New("an access token of at least 32 characters is required")
	}
	if (opts.OAuthIssuer == "") != (opts.OAuthAudience == "") {
		return nil, errors.New("OAuth issuer and audience must be configured together")
	}
	if opts.WebOIDCClientID != "" && opts.OAuthIssuer == "" {
		return nil, errors.New("web OIDC requires an OAuth issuer and audience")
	}
	a := &auth{options: opts, token: sha256.Sum256([]byte(opts.Token)), sessions: make(map[[32]byte]time.Time)}
	if opts.OAuthIssuer != "" {
		for _, value := range []string{opts.OAuthIssuer, opts.OAuthAudience} {
			u, err := url.Parse(value)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return nil, errors.New("OAuth issuer and audience must be absolute HTTPS URLs without credentials, query, or fragment")
			}
		}
		client := http.DefaultClient
		if injected, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok {
			client = injected
		}
		bounded := *client
		if bounded.Timeout == 0 || bounded.Timeout > 10*time.Second {
			bounded.Timeout = 10 * time.Second
		}
		ctx = oidc.ClientContext(ctx, &bounded)
		provider, err := oidc.NewProvider(ctx, opts.OAuthIssuer)
		if err != nil {
			return nil, fmt.Errorf("discover OAuth provider: %w", err)
		}
		a.verifier = provider.VerifierContext(ctx, &oidc.Config{ClientID: opts.OAuthAudience})
		if opts.WebOIDCClientID != "" {
			a.configureBrowser(provider, &bounded)
		}
	}
	return a, nil
}

func (a *auth) validToken(value string) bool {
	got := sha256.Sum256([]byte(value))
	return a.options.Token != "" && subtle.ConstantTimeCompare(got[:], a.token[:]) == 1
}

func (a *auth) authorized(r *http.Request, cookies bool) bool {
	if a.options.Dev {
		return true
	}
	if value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if a.validToken(value) {
			return true
		}
		if a.verifier != nil {
			token, err := a.verifier.Verify(r.Context(), value)
			if err == nil {
				var claims map[string]json.RawMessage
				if token.Claims(&claims) == nil && hasScope(claims, a.options.OAuthScope) {
					return true
				}
			}
		}
	}
	if cookies {
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			key := sha256.Sum256([]byte(cookie.Value))
			a.mu.Lock()
			defer a.mu.Unlock()
			until, ok := a.sessions[key]
			if ok && time.Now().Before(until) {
				return true
			}
			delete(a.sessions, key)
		}
	}
	return false
}

func hasScope(claims map[string]json.RawMessage, required string) bool {
	if required == "" {
		return true
	}
	for _, name := range []string{"scope", "scp"} {
		var text string
		if json.Unmarshal(claims[name], &text) == nil {
			for _, s := range strings.Fields(text) {
				if s == required {
					return true
				}
			}
		}
		var list []string
		if json.Unmarshal(claims[name], &list) == nil {
			for _, s := range list {
				if s == required {
					return true
				}
			}
		}
	}
	return false
}

func (a *auth) challenge(w http.ResponseWriter, r *http.Request) {
	value := `Bearer realm="vrac"`
	if a.options.OAuthAudience != "" {
		u, _ := url.Parse(a.options.OAuthAudience)
		u.Path, u.RawPath = "/.well-known/oauth-protected-resource", ""
		value += fmt.Sprintf(`, resource_metadata=%q, scope=%q`, u.String(), a.options.OAuthScope)
	}
	w.Header().Set("WWW-Authenticate", value)
	writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
}

func (a *auth) metadata(w http.ResponseWriter, r *http.Request) {
	if a.verifier == nil {
		writeError(w, http.StatusNotFound, "not_found", "OAuth is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource": a.options.OAuthAudience, "authorization_servers": []string{a.options.OAuthIssuer},
		"scopes_supported": []string{a.options.OAuthScope}, "bearer_methods_supported": []string{"header"},
	})
}

func (a *auth) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r, a.options.Dev) {
		writeError(w, http.StatusForbidden, "origin", "Cross-origin requests are not allowed")
		return
	}
	a.mu.Lock()
	if time.Now().After(a.reset) {
		a.failures, a.reset = 0, time.Now().Add(time.Minute)
	}
	blocked := a.failures >= 10
	a.mu.Unlock()
	if blocked {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limit", "Too many login attempts; retry in one minute")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !a.validToken(body.Token) && !a.options.Dev {
		a.mu.Lock()
		a.failures++
		a.mu.Unlock()
		a.challenge(w, r)
		return
	}
	if a.startSession(w, http.SameSiteStrictMode) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (a *auth) startSession(w http.ResponseWriter, sameSite http.SameSite) bool {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		writeError(w, http.StatusInternalServerError, "storage", "Could not create a session")
		return false
	}
	value := base64.RawURLEncoding.EncodeToString(random[:])
	key := sha256.Sum256([]byte(value))
	expires := time.Now().Add(12 * time.Hour)
	a.mu.Lock()
	for k, until := range a.sessions {
		if time.Now().After(until) {
			delete(a.sessions, k)
		}
	}
	if len(a.sessions) >= 100 {
		a.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "session_limit", "Too many active sessions")
		return false
	}
	a.sessions[key] = expires
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true,
		Secure: !a.options.Dev, SameSite: sameSite, MaxAge: 12 * 3600, Expires: expires})
	return true
}

func (a *auth) logout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r, a.options.Dev) {
		writeError(w, http.StatusForbidden, "origin", "Cross-origin requests are not allowed")
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		key := sha256.Sum256([]byte(cookie.Value))
		a.mu.Lock()
		delete(a.sessions, key)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: !a.options.Dev, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func sameOrigin(r *http.Request, dev bool) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host && (u.Scheme == "https" || (dev && u.Scheme == "http")) && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
	}
	return true // Non-browser clients use bearer authentication; no CORS is enabled.
}
