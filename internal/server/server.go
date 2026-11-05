// Package server adapts the engine to HTTP, MCP and authentication.
package server

import (
	"fmt"

	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/yan-imensar/vrac/internal/engine"
)

// Options configures the server.
type Options struct {
	// PublicURL is the external base URL, such as https://vrac.example.com.
	// It is the OAuth issuer and the base of the MCP resource.
	PublicURL string
	Version   string
	Timezone  string
	// Web holds the compiled interface; nil serves the API only.
	Web    fs.FS
	Logger *slog.Logger
	// OIDC enables sign-in through an identity provider; nil disables it.
	OIDC *OIDC
}

// Server serves the API, MCP, OAuth and the web interface.
type Server struct {
	engine   *engine.Engine
	opts     Options
	base     *url.URL
	mux      *http.ServeMux
	log      *slog.Logger
	limit    limiter
	webauthn *webauthn.WebAuthn
	flows    flows
	oidc     *oidcClient
}

func init() {
	// The PWA manifest needs its media type; Go does not know it.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// New returns the HTTP handler of a Vrac instance.
func New(e *engine.Engine, opts Options) (*Server, error) {
	base, err := url.Parse(strings.TrimRight(opts.PublicURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, errors.New("public URL must be an absolute http(s) URL")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &Server{engine: e, opts: opts, base: base, mux: http.NewServeMux(), log: opts.Logger}
	if s.webauthn, err = newWebAuthn(base.String(), base.Host); err != nil {
		return nil, fmt.Errorf("passkeys: %w", err)
	}
	if opts.OIDC != nil {
		if opts.OIDC.Issuer == "" || opts.OIDC.ClientID == "" || len(opts.OIDC.Allowed) == 0 {
			return nil, errors.New("OIDC needs an issuer, a client ID and at least one allowed identity")
		}
		if opts.OIDC.Name == "" {
			opts.OIDC.Name = "identity provider"
		}
		s.oidc = &oidcClient{cfg: *opts.OIDC}
		s.opts = opts
	}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) url(path string) string { return s.base.String() + path }

func (s *Server) routes() {
	m := s.mux
	// Authentication.
	m.HandleFunc("GET /api/v1/session", s.session)
	m.HandleFunc("POST /api/v1/setup/start", s.setupStart)
	m.HandleFunc("POST /api/v1/login/start", s.loginStart)
	m.HandleFunc("POST /api/v1/signin/finish", s.finish)
	m.HandleFunc("GET /auth/oidc/start", s.oidcStart)
	m.HandleFunc("GET /auth/oidc/callback", s.oidcCallback)
	m.HandleFunc("GET /api/v1/passkeys", s.api(s.passkeys))
	m.HandleFunc("POST /api/v1/passkeys/start", s.api(s.addPasskeyStart))
	m.HandleFunc("POST /api/v1/passkeys/finish", s.api(s.addPasskeyFinish))
	m.HandleFunc("DELETE /api/v1/passkeys/{id}", s.api(s.deletePasskey))
	m.HandleFunc("POST /api/v1/logout", s.logout)
	m.HandleFunc("POST /api/v1/password", s.api(s.changePassword))
	m.HandleFunc("GET /api/v1/connections", s.api(s.connections))
	m.HandleFunc("POST /api/v1/tokens", s.api(s.createToken))
	m.HandleFunc("DELETE /api/v1/connections/{kind}/{id}", s.api(s.revoke))
	// OAuth authorization server and protected resource.
	m.HandleFunc("GET /.well-known/oauth-authorization-server", s.authServerMetadata)
	m.HandleFunc("GET /.well-known/oauth-protected-resource", s.resourceMetadata)
	m.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.resourceMetadata)
	m.HandleFunc("POST /oauth/register", s.register)
	m.HandleFunc("GET /oauth/authorize", s.authorize)
	m.HandleFunc("GET /api/v1/oauth/request", s.api(s.authorizationRequest))
	m.HandleFunc("POST /api/v1/oauth/decision", s.api(s.decide))
	m.HandleFunc("POST /oauth/token", s.token)
	// Workspace.
	m.HandleFunc("GET /api/v1/today", s.api(s.today))
	m.HandleFunc("GET /api/v1/days", s.api(s.days))
	m.HandleFunc("GET /api/v1/days/{day}", s.api(s.day))
	m.HandleFunc("GET /api/v1/roots", s.api(s.roots))
	m.HandleFunc("GET /api/v1/nodes/{id}", s.api(s.node))
	m.HandleFunc("GET /api/v1/nodes/{id}/children", s.api(s.children))
	m.HandleFunc("GET /api/v1/nodes/{id}/sections", s.api(s.sections))
	m.HandleFunc("POST /api/v1/nodes", s.api(s.create))
	m.HandleFunc("PATCH /api/v1/nodes/{id}", s.api(s.update))
	m.HandleFunc("POST /api/v1/nodes/{id}/move", s.api(s.move))
	m.HandleFunc("POST /api/v1/nodes/{id}/trash", s.api(s.trash))
	m.HandleFunc("POST /api/v1/nodes/{id}/restore", s.api(s.restore))
	m.HandleFunc("POST /api/v1/nodes/{id}/undo", s.api(s.undo))
	m.HandleFunc("POST /api/v1/search", s.api(s.search))
	m.HandleFunc("GET /api/v1/resolve", s.api(s.resolve))
	m.HandleFunc("GET /api/v1/types", s.api(s.types))
	m.HandleFunc("POST /api/v1/types", s.api(s.createType))
	m.HandleFunc("GET /api/v1/types/{name}", s.api(s.typeView))
	m.HandleFunc("GET /api/v1/activity", s.api(s.activity))
	m.HandleFunc("GET /api/v1/export", s.api(s.export))
	m.HandleFunc("GET /api/v1/preferences", s.api(s.preferences))
	m.HandleFunc("PUT /api/v1/preferences", s.api(s.setPreferences))
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "not_found", "unknown endpoint")
	})
	m.Handle("/mcp", s.mcp())
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	if s.opts.Web != nil {
		m.Handle("/", s.web())
	}
}

// web serves the compiled single-page app, falling back to index.html.
func (s *Server) web() http.Handler {
	files := http.FileServerFS(s.opts.Web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" || path == "index.html" {
			s.index(w, r)
			return
		}
		if _, err := fs.Stat(s.opts.Web, path); err != nil {
			s.index(w, r)
			return
		}
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(s.opts.Web, "index.html")
	if err != nil {
		http.Error(w, "the web interface is not built", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
	w.Write(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error      string              `json:"error"`
	Message    string              `json:"message"`
	Unresolved []engine.Unresolved `json:"unresolved,omitempty"`
}

func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Error: code, Message: message})
}

// failEngine maps engine errors to HTTP statuses.
func failEngine(w http.ResponseWriter, err error) {
	var u *engine.UnresolvedError
	switch {
	case errors.As(err, &u):
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "unresolved", Message: err.Error(), Unresolved: u.Items})
	case errors.Is(err, engine.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, engine.ErrConflict):
		fail(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, engine.ErrProtected):
		fail(w, http.StatusForbidden, "protected", err.Error())
	case errors.Is(err, engine.ErrInvalid):
		fail(w, http.StatusBadRequest, "invalid", err.Error())
	default:
		slog.Error("request failed", "error", err)
		fail(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

// limiter slows password guessing: a few failures per minute, globally.
type limiter struct {
	mu       sync.Mutex
	failures []time.Time
}

func (l *limiter) blocked() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	kept := l.failures[:0]
	for _, t := range l.failures {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.failures = kept
	return len(kept) >= 5
}

func (l *limiter) fail() {
	l.mu.Lock()
	l.failures = append(l.failures, time.Now())
	l.mu.Unlock()
}
