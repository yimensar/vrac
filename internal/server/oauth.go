package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/yan-imensar/vrac/internal/engine"
)

// A built-in OAuth 2.1 authorization server: dynamic client registration,
// authorization code with S256 PKCE, refresh token rotation. Clients are
// public; the owner approves each one in the browser.

const (
	accessLifetime  = time.Hour
	refreshLifetime = 90 * 24 * time.Hour
	codeLifetime    = 5 * time.Minute
)

func (s *Server) authServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.url(""),
		"authorization_endpoint":                s.url("/oauth/authorize"),
		"token_endpoint":                        s.url("/oauth/token"),
		"registration_endpoint":                 s.url("/oauth/register"),
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{scope},
	})
}

func (s *Server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.url("/mcp"),
		"authorization_servers":    []string{s.url("")},
		"scopes_supported":         []string{scope},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Vrac",
	})
}

func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

// validRedirect accepts https URLs, loopback http URLs and private-use
// schemes of native apps.
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Scheme == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Host != ""
	case "http":
		host := u.Hostname()
		ip := net.ParseIP(host)
		return host == "localhost" || (ip != nil && ip.IsLoopback())
	case "javascript", "data", "file", "vbscript":
		return false
	}
	return strings.Contains(u.Scheme, ".")
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var b struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "malformed JSON")
		return
	}
	b.ClientName = strings.TrimSpace(b.ClientName)
	if b.ClientName == "" {
		b.ClientName = "MCP client"
	}
	if len(b.ClientName) > 80 {
		b.ClientName = b.ClientName[:80]
	}
	if len(b.RedirectURIs) == 0 || len(b.RedirectURIs) > 10 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "one to ten redirect URIs are required")
		return
	}
	for _, u := range b.RedirectURIs {
		if !validRedirect(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect URIs use https, loopback http or a private-use scheme")
			return
		}
	}
	c, err := s.engine.RegisterClient(r.Context(), b.ClientName, b.RedirectURIs)
	if err != nil {
		failEngine(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"client_id_issued_at":        time.Now().Unix(),
	})
}

// authRequest is a validated authorization request.
type authRequest struct {
	client      engine.OAuthClient
	redirectURI string
	challenge   string
	state       string
	resource    string
}

// parseAuthRequest validates the client and redirect URI first: until they
// are trusted, errors must not redirect.
func (s *Server) parseAuthRequest(ctx context.Context, q url.Values) (authRequest, string, error) {
	var a authRequest
	c, err := s.engineClient(ctx, q.Get("client_id"))
	if err != nil {
		return a, "", errors.New("unknown client")
	}
	a.client = c
	a.redirectURI = q.Get("redirect_uri")
	if !slices.Contains(c.RedirectURIs, a.redirectURI) {
		return a, "", errors.New("redirect_uri is not registered for this client")
	}
	a.state = q.Get("state")
	switch {
	case q.Get("response_type") != "code":
		return a, "unsupported_response_type", errors.New("response_type must be code")
	case q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256":
		return a, "invalid_request", errors.New("PKCE with S256 is required")
	}
	if sc := q.Get("scope"); sc != "" && !slices.Contains(strings.Fields(sc), scope) {
		return a, "invalid_scope", errors.New("the only scope is vrac")
	}
	a.challenge = q.Get("code_challenge")
	a.resource = q.Get("resource")
	if a.resource != "" && a.resource != s.url("/mcp") && a.resource != s.url("") {
		return a, "invalid_target", errors.New("unknown resource")
	}
	if a.resource == "" {
		a.resource = s.url("/mcp")
	}
	return a, "", nil
}

func (s *Server) engineClient(ctx context.Context, id string) (engine.OAuthClient, error) {
	if id == "" {
		return engine.OAuthClient{}, engine.ErrNotFound
	}
	return s.engine.Client(ctx, id)
}

func (a authRequest) redirect(params url.Values) string {
	u, _ := url.Parse(a.redirectURI)
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	if a.state != "" {
		q.Set("state", a.state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// authorize hands a valid request to the web interface, where the owner
// signs in and approves or denies the client.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	a, code, err := s.parseAuthRequest(r.Context(), r.URL.Query())
	if err != nil && code == "" {
		http.Error(w, "Invalid authorization request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Redirect(w, r, a.redirect(url.Values{"error": {code}, "error_description": {err.Error()}}), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/authorize?"+r.URL.RawQuery, http.StatusFound)
}

// authorizationRequest describes a pending request to the consent screen.
func (s *Server) authorizationRequest(w http.ResponseWriter, r *http.Request, p principal) {
	if !p.browser {
		fail(w, http.StatusForbidden, "forbidden", "authorization requires the owner's browser session")
		return
	}
	a, _, err := s.parseAuthRequest(r.Context(), r.URL.Query())
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	u, _ := url.Parse(a.redirectURI)
	writeJSON(w, http.StatusOK, map[string]string{"client_name": a.client.Name, "redirect_host": u.Host, "redirect_uri": a.redirectURI})
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request, p principal) {
	if !p.browser {
		fail(w, http.StatusForbidden, "forbidden", "authorization requires the owner's browser session")
		return
	}
	var b struct {
		Query   string `json:"query"`
		Approve bool   `json:"approve"`
	}
	if !decode(w, r, &b) {
		return
	}
	q, err := url.ParseQuery(b.Query)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid", "malformed query")
		return
	}
	a, code, err := s.parseAuthRequest(r.Context(), q)
	if err != nil && code == "" {
		fail(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"redirect": a.redirect(url.Values{"error": {code}, "error_description": {err.Error()}})})
		return
	}
	if !b.Approve {
		writeJSON(w, http.StatusOK, map[string]string{"redirect": a.redirect(url.Values{"error": {"access_denied"}})})
		return
	}
	authCode := randomToken("")
	if err := s.engine.SaveCode(r.Context(), hashToken(authCode), engine.AuthCode{ClientID: a.client.ID, RedirectURI: a.redirectURI,
		Challenge: a.challenge, Scope: scope, Resource: a.resource}, time.Now().Add(codeLifetime)); err != nil {
		failEngine(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect": a.redirect(url.Values{"code": {authCode}})})
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "malformed form")
		return
	}
	f := r.PostForm
	client, err := s.engineClient(r.Context(), f.Get("client_id"))
	if err != nil {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "unknown client")
		return
	}
	switch f.Get("grant_type") {
	case "authorization_code":
		code, err := s.engine.TakeCode(r.Context(), hashToken(f.Get("code")))
		if err != nil || code.ClientID != client.ID || code.RedirectURI != f.Get("redirect_uri") {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "unknown, expired or mismatched code")
			return
		}
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(code.Challenge)) != 1 {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
			return
		}
		s.issue(w, r, client)
	case "refresh_token":
		hash := hashToken(f.Get("refresh_token"))
		info, err := s.engine.Token(r.Context(), hash)
		if err != nil || info.Kind != engine.TokenRefresh || info.ClientID != client.ID {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "unknown or expired refresh token")
			return
		}
		if err := s.engine.DeleteToken(r.Context(), hash); err != nil {
			failEngine(w, err)
			return
		}
		s.issue(w, r, client)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "authorization_code or refresh_token")
	}
}

func (s *Server) issue(w http.ResponseWriter, r *http.Request, c engine.OAuthClient) {
	access, refresh := randomToken("vat_"), randomToken("vrt_")
	now := time.Now()
	for _, t := range []struct {
		token, kind string
		life        time.Duration
	}{{access, engine.TokenAccess, accessLifetime}, {refresh, engine.TokenRefresh, refreshLifetime}} {
		if err := s.engine.SaveToken(r.Context(), hashToken(t.token), engine.TokenInfo{Kind: t.kind, ClientID: c.ID, Name: c.Name, Scope: scope}, now.Add(t.life)); err != nil {
			failEngine(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": int(accessLifetime.Seconds()),
		"refresh_token": refresh, "scope": scope})
}
