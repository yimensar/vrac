package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/yan-imensar/vrac/internal/engine"
)

const (
	sessionCookie   = "vrac_session"
	sessionLifetime = 30 * 24 * time.Hour
	minPassword     = 10
	// clientHeader must accompany cookie-authenticated mutations. Browsers do
	// not send custom headers cross-site without CORS, which Vrac never
	// grants, so this blocks cross-site request forgery.
	clientHeader = "X-Vrac-Client"
	scope        = "vrac"
)

// principal is an authenticated caller.
type principal struct {
	name    string
	browser bool
}

func randomToken(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Passwords use argon2id with a random salt.
func hashPassword(password string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	key := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$2$65536$2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

func checkPassword(hash, password string) bool {
	var t, m uint32
	var p uint8
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "argon2id" {
		return false
	}
	if _, err := fmt.Sscanf(parts[1]+" "+parts[2]+" "+parts[3], "%d %d %d", &t, &m, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	key, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1
}

// authenticate identifies the caller by bearer token or browser session.
func (s *Server) authenticate(r *http.Request) (principal, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			return principal{}, false
		}
		info, err := s.engine.Token(r.Context(), hashToken(strings.TrimSpace(token)))
		if err != nil || info.Kind == engine.TokenRefresh {
			return principal{}, false
		}
		return principal{name: info.Name}, true
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return principal{}, false
	}
	ok, err := s.engine.SessionValid(r.Context(), hashToken(c.Value))
	if err != nil || !ok {
		return principal{}, false
	}
	return principal{name: "owner", browser: true}, true
}

type apiHandler func(w http.ResponseWriter, r *http.Request, p principal)

// api requires authentication, and the client header for browser mutations.
func (s *Server) api(h apiHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.authenticate(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "unauthorized", "sign in or provide a bearer token")
			return
		}
		if p.browser && r.Method != http.MethodGet && r.Header.Get(clientHeader) == "" {
			fail(w, http.StatusForbidden, "forbidden", "missing "+clientHeader+" header")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		h(w, r, p)
	}
}

// actor names who writes and from which source. Sources default to the
// kind of client.
func (p principal) actor(source string) engine.Actor {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "api"
		if p.browser {
			source = "web"
		}
	}
	if len(source) > 60 {
		source = source[:60]
	}
	return engine.Actor{Name: p.name, Source: source}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid", "malformed JSON: "+err.Error())
		return false
	}
	return true
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	owner, err := s.engine.HasOwner(r.Context())
	if err != nil {
		failEngine(w, err)
		return
	}
	_, authed := s.authenticate(r)
	writeJSON(w, http.StatusOK, map[string]any{"owner": owner, "authenticated": authed, "today": s.engine.Today(),
		"timezone": s.opts.Timezone, "version": s.opts.Version, "mcp_url": s.url("/mcp")})
}

type passwordBody struct {
	Password string `json:"password"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(clientHeader) == "" {
		fail(w, http.StatusForbidden, "forbidden", "missing "+clientHeader+" header")
		return
	}
	var b passwordBody
	if !decode(w, r, &b) {
		return
	}
	if len(b.Password) < minPassword {
		fail(w, http.StatusBadRequest, "invalid", fmt.Sprintf("the password needs at least %d characters", minPassword))
		return
	}
	if err := s.engine.CreateOwner(r.Context(), hashPassword(b.Password)); err != nil {
		failEngine(w, err)
		return
	}
	s.startSession(w, r)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(clientHeader) == "" {
		fail(w, http.StatusForbidden, "forbidden", "missing "+clientHeader+" header")
		return
	}
	if s.limit.blocked() {
		fail(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; wait a minute")
		return
	}
	var b passwordBody
	if !decode(w, r, &b) {
		return
	}
	hash, err := s.engine.OwnerPasswordHash(r.Context())
	if errors.Is(err, engine.ErrNotFound) {
		fail(w, http.StatusConflict, "no_owner", "create the owner account first")
		return
	}
	if err != nil {
		failEngine(w, err)
		return
	}
	if !checkPassword(hash, b.Password) {
		s.limit.fail()
		fail(w, http.StatusUnauthorized, "unauthorized", "wrong password")
		return
	}
	s.startSession(w, r)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	token := randomToken("")
	expires := time.Now().Add(sessionLifetime)
	if err := s.engine.CreateSession(r.Context(), hashToken(token), expires); err != nil {
		failEngine(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: expires, HttpOnly: true,
		Secure: s.base.Scheme == "https", SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.engine.DeleteSession(r.Context(), hashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.base.Scheme == "https", SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, p principal) {
	var b struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !p.browser {
		fail(w, http.StatusForbidden, "forbidden", "only the owner's browser session can change the password")
		return
	}
	if !decode(w, r, &b) {
		return
	}
	hash, err := s.engine.OwnerPasswordHash(r.Context())
	if err != nil {
		failEngine(w, err)
		return
	}
	if !checkPassword(hash, b.Current) {
		s.limit.fail()
		fail(w, http.StatusUnauthorized, "unauthorized", "wrong current password")
		return
	}
	if len(b.Password) < minPassword {
		fail(w, http.StatusBadRequest, "invalid", fmt.Sprintf("the password needs at least %d characters", minPassword))
		return
	}
	if err := s.engine.SetOwnerPassword(r.Context(), hashPassword(b.Password)); err != nil {
		failEngine(w, err)
		return
	}
	s.startSession(w, r)
}

func (s *Server) connections(w http.ResponseWriter, r *http.Request, p principal) {
	list, err := s.engine.Connections(r.Context())
	if err != nil {
		failEngine(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": list})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request, p principal) {
	if !p.browser {
		fail(w, http.StatusForbidden, "forbidden", "tokens are created from the owner's browser session")
		return
	}
	var b struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &b) {
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || len(b.Name) > 60 || b.Name == "owner" {
		fail(w, http.StatusBadRequest, "invalid", "token names have 1 to 60 characters and cannot be owner")
		return
	}
	token := randomToken("vrac_")
	if err := s.engine.SaveToken(r.Context(), hashToken(token), engine.TokenInfo{Kind: engine.TokenPersonal, Name: b.Name, Scope: scope}, time.Time{}); err != nil {
		failEngine(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": token, "name": b.Name})
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request, p principal) {
	if !p.browser {
		fail(w, http.StatusForbidden, "forbidden", "connections are revoked from the owner's browser session")
		return
	}
	if err := s.engine.Revoke(r.Context(), r.PathValue("kind"), r.PathValue("id")); err != nil {
		failEngine(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// ResetPassword sets the owner's password from the command line, creating
// the owner if needed, and ends every browser session.
func ResetPassword(ctx context.Context, e *engine.Engine, password string) error {
	if len(password) < minPassword {
		return fmt.Errorf("the password needs at least %d characters", minPassword)
	}
	owner, err := e.HasOwner(ctx)
	if err != nil {
		return err
	}
	if !owner {
		return e.CreateOwner(ctx, hashPassword(password))
	}
	return e.SetOwnerPassword(ctx, hashPassword(password))
}
