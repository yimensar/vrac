package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-oauth2/oauth2/v4"
	oautherrors "github.com/go-oauth2/oauth2/v4/errors"
	"github.com/go-oauth2/oauth2/v4/manage"
	oauthserver "github.com/go-oauth2/oauth2/v4/server"
	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	PublicURL     string
	OwnerPassword string
	DevOrigin     string
}
type Auth struct {
	storage  *sqlStorage
	cfg      Config
	OAuth    *oauthserver.Server
	mu       sync.Mutex
	attempts map[string][]time.Time
}
type scopesKey struct{}

func New(db *sql.DB, cfg Config) (*Auth, error) {
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname()))) {
		return nil, errors.New("public URL must use HTTPS, or HTTP on localhost, without a path")
	}
	if cfg.DevOrigin != "" {
		d, e := url.Parse(cfg.DevOrigin)
		if e != nil || !isLoopback(d.Hostname()) || !isLoopback(u.Hostname()) {
			return nil, errors.New("development origin is only allowed on localhost")
		}
	}
	a := &Auth{storage: &sqlStorage{db: db, resource: cfg.PublicURL + "/mcp"}, cfg: cfg, attempts: make(map[string][]time.Time)}
	if err = a.storage.init(context.Background()); err != nil {
		return nil, err
	}
	if cfg.OwnerPassword != "" && a.NeedsSetup() {
		if err = a.setup(context.Background(), cfg.OwnerPassword); err != nil {
			return nil, err
		}
	}
	manager := manage.NewDefaultManager()
	manager.MapClientStorage(a.storage)
	manager.MapTokenStorage(a.storage)
	manager.SetAuthorizeCodeExp(5 * time.Minute)
	manager.SetAuthorizeCodeTokenCfg(&manage.Config{AccessTokenExp: time.Hour, RefreshTokenExp: 30 * 24 * time.Hour, IsGenerateRefresh: true})
	manager.SetRefreshTokenCfg(&manage.RefreshingConfig{AccessTokenExp: time.Hour, RefreshTokenExp: 30 * 24 * time.Hour, IsGenerateRefresh: true, IsResetRefreshTime: true, IsRemoveAccess: true, IsRemoveRefreshing: true})
	manager.SetValidateURIHandler(func(domain, redirect string) error {
		var uris []string
		if json.Unmarshal([]byte(domain), &uris) != nil {
			return errors.New("invalid client")
		}
		for _, v := range uris {
			if v == redirect {
				return nil
			}
		}
		return errors.New("redirect URI must exactly match a registered URI")
	})
	cfgOAuth := oauthserver.NewConfig()
	cfgOAuth.AllowedGrantTypes = []oauth2.GrantType{oauth2.AuthorizationCode, oauth2.Refreshing}
	cfgOAuth.AllowedResponseTypes = []oauth2.ResponseType{oauth2.Code}
	cfgOAuth.ForcePKCE = true
	cfgOAuth.AllowedCodeChallengeMethods = []oauth2.CodeChallengeMethod{oauth2.CodeChallengeS256}
	a.OAuth = oauthserver.NewServer(cfgOAuth, manager)
	a.OAuth.SetResponseErrorHandler(func(_ context.Context, response *oautherrors.Response) {
		if response.Error == oautherrors.ErrInvalidGrant || response.Error == oautherrors.ErrUnsupportedGrantType {
			response.StatusCode = http.StatusBadRequest
		}
	})
	a.OAuth.SetClientInfoHandler(oauthserver.ClientFormHandler)
	a.OAuth.SetAccessTokenResolveHandler(func(r *http.Request) (string, bool) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return "", false
		}
		return parts[1], true
	})
	a.OAuth.SetUserAuthorizationHandler(func(w http.ResponseWriter, r *http.Request) (string, error) {
		if r.Context().Value(approvedKey{}) != true {
			return "", errors.New("consent required")
		}
		return "owner", nil
	})
	a.OAuth.SetClientScopeHandler(func(tgr *oauth2.TokenGenerateRequest) (bool, error) {
		return tgr.Scope == "" || validScopes(tgr.Scope), nil
	})
	a.OAuth.SetRefreshingScopeHandler(func(tgr *oauth2.TokenGenerateRequest, old string) (bool, error) {
		for _, v := range strings.Fields(tgr.Scope) {
			if !hasScope(old, v) {
				return false, nil
			}
		}
		return true, nil
	})
	return a, nil
}
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func validScopes(s string) bool {
	f := strings.Fields(s)
	if len(f) == 0 {
		return false
	}
	for _, v := range f {
		if v != "notes:read" && v != "notes:write" {
			return false
		}
	}
	return true
}
func hasScope(s, want string) bool {
	for _, v := range strings.Fields(s) {
		if v == want {
			return true
		}
	}
	return false
}
func HasScope(ctx context.Context, want string) bool {
	s, _ := ctx.Value(scopesKey{}).(string)
	return hasScope(s, want)
}
func (a *Auth) PublicURL() string { return a.cfg.PublicURL }
func (a *Auth) Resource() string  { return a.storage.resource }
func (a *Auth) NeedsSetup() bool {
	var n int
	return a.storage.db.QueryRow("SELECT count(*) FROM auth_owner").Scan(&n) == nil && n == 0
}
func (a *Auth) setup(ctx context.Context, password string) error {
	if len(password) < 12 || len(password) > 72 {
		return errors.New("password must contain 12 to 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = a.storage.db.ExecContext(ctx, "INSERT INTO auth_owner(id,password_hash) VALUES(1,?)", hash)
	return err
}
func (a *Auth) passwordOK(r *http.Request, password string) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !a.allowAttempt("login:"+host, 8) {
		return false
	}
	var hash []byte
	err := a.storage.db.QueryRowContext(r.Context(), "SELECT password_hash FROM auth_owner WHERE id=1").Scan(&hash)
	return err == nil && bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

// Bound both key count and per-key slices; expire old keys before admitting new IPs.
func (a *Auth) allowAttempt(key string, limit int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, attempts := range a.attempts {
		if len(attempts) == 0 || now.Sub(attempts[len(attempts)-1]) >= time.Minute {
			delete(a.attempts, k)
		}
	}
	old := a.attempts[key]
	if len(old) == 0 && len(a.attempts) >= 1024 {
		return false
	}
	valid := old[:0]
	for _, v := range old {
		if now.Sub(v) < time.Minute {
			valid = append(valid, v)
		}
	}
	if len(valid) >= limit {
		a.attempts[key] = valid
		return false
	}
	a.attempts[key] = append(valid, now)
	return true
}
func (a *Auth) Session(r *http.Request) bool {
	c, err := r.Cookie("vrac_session")
	if err != nil {
		return false
	}
	var exp int64
	return a.storage.db.QueryRowContext(r.Context(), "SELECT expires FROM auth_sessions WHERE token_hash=?", digest(c.Value)).Scan(&exp) == nil && exp > time.Now().Unix()
}
func (a *Auth) newSession(w http.ResponseWriter, r *http.Request) error {
	token := randomToken()
	_, err := a.storage.db.ExecContext(r.Context(), "INSERT INTO auth_sessions(token_hash,expires) VALUES(?,?)", digest(token), time.Now().Add(30*24*time.Hour).Unix())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "vrac_session", Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
	return nil
}
func (a *Auth) OriginOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" {
		return origin == a.cfg.PublicURL || (a.cfg.DevOrigin != "" && origin == a.cfg.DevOrigin)
	}
	return r.Header.Get("Sec-Fetch-Site") != "cross-site"
}
func (a *Auth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Session(r) {
			jsonError(w, 401, "authentication required")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && !a.OriginOK(r) {
			jsonError(w, 403, "origin not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *Auth) RequireMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.OriginOK(r) {
			jsonError(w, 403, "origin not allowed")
			return
		}
		info, err := a.OAuth.ValidationBearerToken(r)
		if err != nil || info.GetUserID() != "owner" || !hasScope(info.GetScope(), "notes:read") {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp", scope="notes:read"`, a.cfg.PublicURL))
			jsonError(w, 401, "valid MCP OAuth token required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scopesKey{}, info.GetScope())))
	})
}
func jsonError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
func jsonReply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("application/json required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func (a *Auth) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		yes := a.Session(r)
		out := map[string]any{"authenticated": yes, "needsSetup": a.NeedsSetup(), "mcp": map[string]any{"url": a.Resource(), "oauth": true}}
		if yes {
			out["user"] = map[string]string{"name": "Yan"}
		}
		jsonReply(w, 200, out)
	})
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		if !a.OriginOK(r) {
			jsonError(w, 403, "origin not allowed")
			return
		}
		if !a.NeedsSetup() {
			jsonError(w, 409, "owner already configured")
			return
		}
		var in struct {
			Password string `json:"password"`
		}
		if readJSON(w, r, &in) != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if err := a.setup(r.Context(), in.Password); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		if a.newSession(w, r) != nil {
			jsonError(w, 500, "could not create session")
			return
		}
		jsonReply(w, 200, map[string]bool{"authenticated": true})
	})
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		if !a.OriginOK(r) {
			jsonError(w, 403, "origin not allowed")
			return
		}
		var in struct {
			Password string `json:"password"`
		}
		if readJSON(w, r, &in) != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
		if !a.passwordOK(r, in.Password) {
			jsonError(w, 401, "incorrect password or too many attempts; retry in a minute")
			return
		}
		if a.newSession(w, r) != nil {
			jsonError(w, 500, "could not create session")
			return
		}
		jsonReply(w, 200, map[string]bool{"authenticated": true})
	})
	mux.Handle("POST /api/logout", a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("vrac_session"); err == nil {
			_, _ = a.storage.db.ExecContext(r.Context(), "DELETE FROM auth_sessions WHERE token_hash=?", digest(c.Value))
		}
		http.SetCookie(w, &http.Cookie{Name: "vrac_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode})
		jsonReply(w, 200, map[string]bool{"authenticated": false})
	})))
	a.registerOAuth(mux)
}
func equal(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
