package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDC lets the owner sign in through an identity provider such as Pocket
// ID, instead of the local account. Only identities listed in Allowed (a
// subject, or a verified email) open the instance.
type OIDC struct {
	Issuer       string
	ClientID     string
	ClientSecret string // empty for a public client with PKCE
	Name         string // shown on the sign-in button
	Allowed      []string
}

const oidcCookie = "vrac_oidc"

type oidcClient struct {
	cfg      OIDC
	mu       sync.Mutex
	provider *oidc.Provider
	pending  flows
}

// discover loads the provider metadata on first use, so a provider that is
// down does not stop Vrac from starting.
func (c *oidcClient) discover(ctx context.Context) (*oidc.Provider, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider != nil {
		return c.provider, nil
	}
	p, err := oidc.NewProvider(ctx, c.cfg.Issuer)
	if err == nil {
		c.provider = p
	}
	return p, err
}

func (s *Server) oauth2Config(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{ClientID: s.oidc.cfg.ClientID, ClientSecret: s.oidc.cfg.ClientSecret, Endpoint: p.Endpoint(),
		RedirectURL: s.url("/auth/oidc/callback"), Scopes: []string{oidc.ScopeOpenID, "email", "profile"}}
}

// safeReturn keeps returns on this site.
func safeReturn(path string) string {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.HasPrefix(path, "/\\") {
		return "/"
	}
	return path
}

func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.oidc.discover(r.Context())
	if err != nil {
		http.Error(w, "The identity provider is unreachable.", http.StatusBadGateway)
		return
	}
	verifier, nonce := oauth2.GenerateVerifier(), randomToken("")
	state := s.oidc.pending.put(flow{kind: "oidc", back: safeReturn(r.URL.Query().Get("return")), verifier: verifier, nonce: nonce})
	// The state is bound to this browser: the callback must carry both.
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: state, Path: "/auth/oidc", MaxAge: int(flowLifetime.Seconds()),
		HttpOnly: true, Secure: s.base.Scheme == "https", SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, s.oauth2Config(p).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)), http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.NotFound(w, r)
		return
	}
	deny := func(msg string) {
		http.Error(w, "Sign-in failed: "+msg, http.StatusForbidden)
	}
	state := r.URL.Query().Get("state")
	c, err := r.Cookie(oidcCookie)
	if err != nil || c.Value != state {
		deny("this sign-in did not start in this browser")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: s.base.Scheme == "https", SameSite: http.SameSiteLaxMode})
	fl, ok := s.oidc.pending.take(state)
	if !ok {
		deny("expired; start again")
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		deny(e)
		return
	}
	p, err := s.oidc.discover(r.Context())
	if err != nil {
		http.Error(w, "The identity provider is unreachable.", http.StatusBadGateway)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tok, err := s.oauth2Config(p).Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(fl.verifier))
	if err != nil {
		deny("the code was refused")
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	id, err := p.Verifier(&oidc.Config{ClientID: s.oidc.cfg.ClientID}).Verify(ctx, raw)
	if err != nil || id.Nonce != fl.nonce {
		deny("the identity token is invalid")
		return
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if err := id.Claims(&claims); err != nil {
		deny("unreadable identity")
		return
	}
	if !s.oidc.allowed(id.Subject, claims.Email, claims.Verified) {
		deny("this identity may not open this Vrac")
		return
	}
	token := randomToken("")
	expires := time.Now().Add(sessionLifetime)
	if err := s.engine.CreateSession(r.Context(), hashToken(token), expires); err != nil {
		failEngine(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: expires, HttpOnly: true,
		Secure: s.base.Scheme == "https", SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, fl.back, http.StatusFound)
}

func (c *oidcClient) allowed(subject, email string, verified bool) bool {
	for _, a := range c.cfg.Allowed {
		a = strings.TrimSpace(a)
		if a != "" && (a == subject || (verified && strings.EqualFold(a, email))) {
			return true
		}
	}
	return false
}
