package auth

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-oauth2/oauth2/v4/models"
)

type approvedKey struct{}

func (a *Auth) registerOAuth(mux *http.ServeMux) {
	metadata := func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, 200, map[string]any{"issuer": a.cfg.PublicURL, "authorization_endpoint": a.cfg.PublicURL + "/oauth/authorize", "token_endpoint": a.cfg.PublicURL + "/oauth/token", "registration_endpoint": a.cfg.PublicURL + "/oauth/register", "revocation_endpoint": a.cfg.PublicURL + "/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{"notes:read", "notes:write"}, "authorization_response_iss_parameter_supported": false})
	}
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", metadata)
	resource := func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, 200, map[string]any{"resource": a.Resource(), "authorization_servers": []string{a.cfg.PublicURL}, "scopes_supported": []string{"notes:read", "notes:write"}, "bearer_methods_supported": []string{"header"}, "resource_name": "Vrac notes"})
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", resource)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", resource)
	mux.HandleFunc("POST /oauth/register", a.registerClient)
	mux.HandleFunc("GET /oauth/authorize", a.authorize)
	mux.HandleFunc("POST /oauth/authorize", a.authorize)
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if r.ParseForm() != nil {
			jsonError(w, 400, "invalid form")
			return
		}
		// Refresh remains bound to the original stored audience. Standard OAuth
		// TokenSource implementations omit resource when rotating a refresh token.
		resource := r.FormValue("resource")
		refreshing := r.FormValue("grant_type") == "refresh_token"
		if resource != a.Resource() && !(refreshing && resource == "") {
			jsonError(w, 400, "resource must match the MCP resource URL")
			return
		}
		if r.FormValue("grant_type") == "authorization_code" && r.FormValue("code_verifier") == "" {
			jsonError(w, 400, "PKCE verifier required")
			return
		}
		// Serialize code redemption and refresh rotation: the library uses separate load/remove calls.
		a.mu.Lock()
		defer a.mu.Unlock()
		// The library refresh manager loads the original client from the token;
		// explicitly bind the request's public client ID before allowing rotation.
		if r.FormValue("grant_type") == "refresh_token" {
			info, err := a.storage.GetByRefresh(r.Context(), r.FormValue("refresh_token"))
			if err != nil || info == nil || info.GetClientID() != r.FormValue("client_id") {
				jsonError(w, http.StatusBadRequest, "invalid_grant")
				return
			}
		}
		if err := a.OAuth.HandleTokenRequest(w, r); err != nil {
			jsonError(w, 400, "invalid OAuth request")
		}
	})
	mux.HandleFunc("POST /oauth/revoke", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if r.ParseForm() != nil {
			jsonError(w, 400, "invalid form")
			return
		}
		clientID := r.FormValue("client_id")
		if clientID == "" {
			jsonError(w, 400, "client_id required")
			return
		}
		token := r.FormValue("token")
		a.mu.Lock()
		defer a.mu.Unlock()
		// Public clients can only revoke a token issued to their own registration.
		info, err := a.storage.GetByAccess(r.Context(), token)
		if err != nil || info == nil {
			info, err = a.storage.GetByRefresh(r.Context(), token)
		}
		if err == nil && info != nil && info.GetClientID() == clientID {
			_, err = a.storage.db.ExecContext(r.Context(), "DELETE FROM auth_tokens WHERE access_hash=? OR refresh_hash=?", digest(token), digest(token))
			if err != nil {
				jsonError(w, 500, "revocation failed")
				return
			}
		}
		jsonReply(w, 200, map[string]bool{"ok": true})
	})
	mux.Handle("POST /api/connections/revoke", a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if _, err := a.storage.db.ExecContext(r.Context(), "DELETE FROM auth_tokens"); err != nil {
			jsonError(w, 500, "revocation failed")
			return
		}
		jsonReply(w, 200, map[string]bool{"ok": true})
	})))
}
func (a *Auth) registerClient(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !a.allowAttempt("register:"+host, 8) || !a.allowAttempt("register:global", 64) {
		jsonError(w, 429, "too many client registrations; retry in a minute")
		return
	}
	var in struct {
		ClientName    string   `json:"client_name"`
		RedirectURIs  []string `json:"redirect_uris"`
		GrantTypes    []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
		Method        string   `json:"token_endpoint_auth_method"`
		Scope         string   `json:"scope"`
	}
	// OAuth registration metadata is extensible, so tolerate unrelated standard fields.
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		jsonError(w, 400, "invalid registration")
		return
	}
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 8 || (in.Method != "" && in.Method != "none") {
		jsonError(w, 400, "public client with redirect_uris and auth method none required")
		return
	}
	for _, v := range in.RedirectURIs {
		u, err := url.Parse(v)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname()))) {
			jsonError(w, 400, "redirect URIs must use HTTPS or loopback HTTP")
			return
		}
	}
	for _, v := range in.GrantTypes {
		if v != "authorization_code" && v != "refresh_token" {
			jsonError(w, 400, "unsupported grant type")
			return
		}
	}
	for _, v := range in.ResponseTypes {
		if v != "code" {
			jsonError(w, 400, "unsupported response type")
			return
		}
	}
	if in.Scope != "" && !validScopes(in.Scope) {
		jsonError(w, 400, "invalid scope")
		return
	}
	// Keep cleanup, the capacity check, and creation atomic within this process.
	a.mu.Lock()
	defer a.mu.Unlock()
	var count int
	// Reclaim abandoned public registrations before applying the capacity limit.
	// Registrations with live authorization codes or tokens remain available.
	if err := a.storage.pruneRegistrations(r.Context(), time.Now()); err != nil {
		jsonError(w, 500, "could not clean up expired registrations")
		return
	}
	if a.storage.db.QueryRowContext(r.Context(), "SELECT count(*) FROM auth_clients").Scan(&count) != nil || count >= 512 {
		jsonError(w, 429, "registration capacity reached")
		return
	}
	raw, _ := json.Marshal(in.RedirectURIs)
	id := randomToken()
	if err := a.storage.saveClient(r.Context(), &models.Client{ID: id, Domain: string(raw), Public: true, UserID: "owner"}); err != nil {
		jsonError(w, 500, "could not register client")
		return
	}
	jsonReply(w, 201, map[string]any{"client_id": id, "client_name": in.ClientName, "redirect_uris": in.RedirectURIs, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none", "client_id_issued_at": time.Now().Unix()})
}
func (a *Auth) validateAuthorize(r *http.Request) error {
	if r.FormValue("resource") != a.Resource() {
		return errors.New("resource must match the MCP resource URL")
	}
	if r.FormValue("state") == "" {
		return errors.New("OAuth state is required")
	}
	if r.FormValue("response_type") != "code" || r.FormValue("code_challenge_method") != "S256" || len(r.FormValue("code_challenge")) != 43 {
		return errors.New("authorization code with PKCE S256 is required")
	}
	scope := r.FormValue("scope")
	if scope == "" {
		scope = "notes:read"
		r.Form.Set("scope", scope)
	}
	if !validScopes(scope) || !hasScope(scope, "notes:read") {
		return errors.New("notes:read is required; notes:write is optional")
	}
	c, err := a.storage.GetByID(r.Context(), r.FormValue("client_id"))
	if err != nil || c == nil {
		return errors.New("unknown OAuth client")
	}
	var redirects []string
	if json.Unmarshal([]byte(c.GetDomain()), &redirects) != nil {
		return errors.New("invalid OAuth client")
	}
	for _, v := range redirects {
		if v == r.FormValue("redirect_uri") {
			return nil
		}
	}
	return errors.New("redirect URI must exactly match registration")
}

var consentTemplate = template.Must(template.New("consent").Parse(`<!doctype html><html lang="fr"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Connecter Vrac</title><style>body{font:16px system-ui;background:#f7f7f2;color:#243a30;display:grid;place-items:center;min-height:95vh;margin:0}main{max-width:420px;padding:36px;background:white;border:1px solid #e6e8df;border-radius:24px;margin:20px}h1{font-size:26px;letter-spacing:-1px}p{line-height:1.6;color:#5d6b62}code{overflow-wrap:anywhere}input{box-sizing:border-box;width:100%;padding:13px;border:1px solid #ccd5cb;border-radius:10px;margin:12px 0}button{padding:13px 20px;border:0;border-radius:10px;background:#325e49;color:white;cursor:pointer;font:inherit}button[value=deny]{background:#edf0e9;color:#344b3c}small{display:block;margin-top:24px;color:#79857b}</style><main><strong>vrac<span style="color:#799b7e">.</span></strong><h1>Votre mémoire, à portée de l’IA.</h1><p>Autoriser cette application à accéder à vos notes Vrac ?</p><p><code>{{.Client}}</code></p><p>Permissions : <strong>{{.Scopes}}</strong></p><form method="post" action="{{.Action}}"><input type="hidden" name="csrf" value="{{.CSRF}}">{{if .Login}}<label>Mot de passe Vrac<input type="password" name="password" autocomplete="current-password" required minlength="12"></label>{{end}}<button name="decision" value="approve">Autoriser</button> <button name="decision" value="deny" formnovalidate>Annuler</button></form><small>Vous pourrez révoquer cet accès depuis Vrac. Le contenu de vos notes reste sur votre serveur.</small></main></html>`))

func (a *Auth) authorize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if r.ParseForm() != nil {
		jsonError(w, 400, "invalid request")
		return
	}
	if err := a.validateAuthorize(r); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if a.NeedsSetup() {
		jsonError(w, 409, "configure the Vrac owner before connecting a client")
		return
	}
	if r.Method == "GET" {
		csrf := randomToken()
		http.SetCookie(w, &http.Cookie{Name: "vrac_oauth_csrf", Value: csrf, Path: "/oauth/authorize", HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = consentTemplate.Execute(w, map[string]any{"Client": r.FormValue("client_id"), "Scopes": r.FormValue("scope"), "Action": r.URL.RequestURI(), "CSRF": csrf, "Login": !a.Session(r)})
		return
	}
	c, err := r.Cookie("vrac_oauth_csrf")
	if err != nil || !equal(c.Value, r.PostFormValue("csrf")) || !a.OriginOK(r) {
		jsonError(w, 403, "invalid consent token or origin")
		return
	}
	if r.PostFormValue("decision") != "approve" {
		u, _ := url.Parse(r.FormValue("redirect_uri"))
		q := u.Query()
		q.Set("error", "access_denied")
		q.Set("state", r.FormValue("state"))
		u.RawQuery = q.Encode()
		http.Redirect(w, r, u.String(), http.StatusSeeOther)
		return
	}
	if !a.Session(r) {
		if !a.passwordOK(r, r.PostFormValue("password")) {
			jsonError(w, 401, "incorrect password or too many attempts")
			return
		}
		if a.newSession(w, r) != nil {
			jsonError(w, 500, "could not create session")
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "vrac_oauth_csrf", Path: "/oauth/authorize", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode})
	ctx := context.WithValue(r.Context(), approvedKey{}, true)
	if err := a.OAuth.HandleAuthorizeRequest(w, r.WithContext(ctx)); err != nil {
		jsonError(w, 400, "authorization failed")
	}
}
