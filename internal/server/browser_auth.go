package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const loginCookie = "vrac_login"

type browserAuth struct {
	config        oauth2.Config
	verifier      *oidc.IDTokenVerifier
	client        *http.Client
	requireIssuer bool
}

type browserFlow struct {
	verifier, nonce string
	expires         time.Time
}

func (a *auth) configureBrowser(provider *oidc.Provider, client *http.Client) {
	callback, _ := url.Parse(a.options.OAuthAudience)
	callback.Path, callback.RawPath = "/auth/callback", ""
	endpoint := provider.Endpoint()
	var metadata struct {
		IssuerParameter bool `json:"authorization_response_iss_parameter_supported"`
	}
	_ = provider.Claims(&metadata)
	endpoint.AuthStyle = oauth2.AuthStyleInParams // Public client: client_id and PKCE, no secret.
	a.browser = &browserAuth{
		config: oauth2.Config{ClientID: a.options.WebOIDCClientID, Endpoint: endpoint,
			RedirectURL: callback.String(), Scopes: []string{oidc.ScopeOpenID, a.options.OAuthScope}},
		verifier:      provider.VerifierContext(oidc.ClientContext(context.Background(), client), &oidc.Config{ClientID: a.options.WebOIDCClientID}),
		client:        client,
		requireIssuer: metadata.IssuerParameter,
	}
	a.flows = make(map[[32]byte]browserFlow)
}

func (a *auth) browserConfig(w http.ResponseWriter, r *http.Request) {
	label := strings.TrimSpace(a.options.WebOIDCProviderName)
	if label == "" {
		label = "Single sign-on"
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": a.browser != nil, "label": label})
}

func (a *auth) browserLogin(w http.ResponseWriter, r *http.Request) {
	if a.browser == nil {
		writeError(w, http.StatusNotFound, "not_found", "Browser OIDC is not configured")
		return
	}
	if !sameOrigin(r, a.options.Dev) {
		writeError(w, http.StatusForbidden, "origin", "Cross-origin requests are not allowed")
		return
	}
	state := oauth2.GenerateVerifier()
	flow := browserFlow{verifier: oauth2.GenerateVerifier(), nonce: oauth2.GenerateVerifier(), expires: time.Now().Add(10 * time.Minute)}
	a.mu.Lock()
	for key, previous := range a.flows {
		if time.Now().After(previous.expires) {
			delete(a.flows, key)
		}
	}
	if len(a.flows) >= 100 {
		a.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "rate_limit", "Too many pending sign-ins; retry later")
		return
	}
	a.flows[sha256.Sum256([]byte(state))] = flow
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: loginCookie, Value: state, Path: "/auth", HttpOnly: true,
		Secure: !a.options.Dev, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	location := a.browser.config.AuthCodeURL(state, oidc.Nonce(flow.nonce), oauth2.S256ChallengeOption(flow.verifier),
		oauth2.SetAuthURLParam("resource", a.options.OAuthAudience))
	http.Redirect(w, r, location, http.StatusFound)
}

func (a *auth) browserCallback(w http.ResponseWriter, r *http.Request) {
	if a.browser == nil {
		writeError(w, http.StatusNotFound, "not_found", "Browser OIDC is not configured")
		return
	}
	failed := func() { http.Redirect(w, r, "/?auth_error=signin_failed", http.StatusFound) }
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(loginCookie)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		failed()
		return
	}
	key := sha256.Sum256([]byte(state))
	a.mu.Lock()
	flow, ok := a.flows[key]
	delete(a.flows, key) // Single use, including provider errors and failed exchanges.
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: loginCookie, Path: "/auth", MaxAge: -1, HttpOnly: true,
		Secure: !a.options.Dev, SameSite: http.SameSiteLaxMode})
	returnedIssuer := r.URL.Query().Get("iss")
	if !ok || time.Now().After(flow.expires) || r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" || (a.browser.requireIssuer && returnedIssuer == "") || (returnedIssuer != "" && returnedIssuer != a.options.OAuthIssuer) {
		failed()
		return
	}
	ctx := oidc.ClientContext(r.Context(), a.browser.client)
	tokens, err := a.browser.config.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(flow.verifier), oauth2.SetAuthURLParam("resource", a.options.OAuthAudience))
	if err != nil {
		failed()
		return
	}
	rawID, _ := tokens.Extra("id_token").(string)
	id, err := a.browser.verifier.Verify(ctx, rawID)
	if err != nil || id.Subject == "" || id.Nonce != flow.nonce {
		failed()
		return
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(tokens.AccessToken) != nil {
		failed()
		return
	}
	access, err := a.verifier.Verify(ctx, tokens.AccessToken)
	if err != nil || access.Subject != id.Subject {
		failed()
		return
	}
	var claims map[string]json.RawMessage
	if access.Claims(&claims) != nil || !hasScope(claims, a.options.OAuthScope) {
		failed()
		return
	}
	// Lax lets the session follow the provider's cross-site return redirect.
	// Browser mutations additionally require the same Origin.
	if a.startSession(w, http.SameSiteLaxMode) {
		http.Redirect(w, r, "/", http.StatusFound)
	}
}
