package server

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/yan-imensar/vrac/internal/engine"
)

// Signing in locally takes a username and a password, then a passkey. The
// first sign-in after a recovery, or of an account created from the command
// line, enrolls a passkey instead. Setup creates the account with both.

const flowLifetime = 5 * time.Minute

// flow is a sign-in or registration ceremony in progress.
type flow struct {
	kind    string // setup, login, enroll, add, oidc
	session webauthn.SessionData
	owner   engine.Owner // setup: the account to create
	// oidc: where to go back, and the PKCE verifier and nonce to check.
	back, verifier, nonce string
	expires               time.Time
}

type flows struct {
	mu sync.Mutex
	m  map[string]flow
}

func (f *flows) put(fl flow) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[string]flow{}
	}
	now := time.Now()
	for k, v := range f.m {
		if now.After(v.expires) {
			delete(f.m, k)
		}
	}
	id := randomToken("")
	fl.expires = now.Add(flowLifetime)
	f.m[id] = fl
	return id
}

// take returns a flow once.
func (f *flows) take(id string) (flow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, ok := f.m[id]
	delete(f.m, id)
	return fl, ok && time.Now().Before(fl.expires)
}

// user adapts the owner to WebAuthn.
type user struct {
	owner engine.Owner
	creds []webauthn.Credential
}

func (u user) WebAuthnID() []byte                         { return u.owner.UserHandle }
func (u user) WebAuthnName() string                       { return u.owner.Username }
func (u user) WebAuthnDisplayName() string                { return u.owner.Username }
func (u user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func newWebAuthn(base string, host string) (*webauthn.WebAuthn, error) {
	id := host
	if i := strings.LastIndexByte(id, ':'); i > 0 && !strings.Contains(id, "]") {
		id = id[:i]
	}
	if net.ParseIP(strings.Trim(id, "[]")) != nil {
		return nil, fmt.Errorf("the public URL %s uses an IP address; passkeys need a domain name, such as localhost", base)
	}
	return webauthn.New(&webauthn.Config{RPID: id, RPDisplayName: "Vrac", RPOrigins: []string{base}})
}

func (s *Server) ownerUser(r *http.Request) (user, []engine.Passkey, error) {
	o, err := s.engine.Owner(r.Context())
	if err != nil {
		return user{}, nil, err
	}
	keys, err := s.engine.Passkeys(r.Context())
	if err != nil {
		return user{}, nil, err
	}
	u := user{owner: o}
	for _, k := range keys {
		var c webauthn.Credential
		if err := json.Unmarshal(k.Credential, &c); err != nil {
			return user{}, nil, err
		}
		u.creds = append(u.creds, c)
	}
	return u, keys, nil
}

func browserOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(clientHeader) == "" {
		fail(w, http.StatusForbidden, "forbidden", "missing "+clientHeader+" header")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	return true
}

type credentialsBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ceremony answers the start of a WebAuthn ceremony.
type ceremony struct {
	Flow    string `json:"flow"`
	Mode    string `json:"mode"` // create or get
	Options any    `json:"options"`
}

func validUsername(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 || strings.ContainsAny(name, " \t\n") {
		return errors.New("usernames have 1 to 60 characters and no spaces")
	}
	return nil
}

// setupStart begins creating the owner account: it checks the username and
// password and asks for a passkey. Nothing is stored before it is created.
func (s *Server) setupStart(w http.ResponseWriter, r *http.Request) {
	if !browserOnly(w, r) {
		return
	}
	var b credentialsBody
	if !decode(w, r, &b) {
		return
	}
	if exists, err := s.engine.HasOwner(r.Context()); err != nil || exists {
		fail(w, http.StatusConflict, "conflict", "the owner account already exists")
		return
	}
	if err := validUsername(b.Username); err != nil {
		fail(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if len(b.Password) < minPassword {
		fail(w, http.StatusBadRequest, "invalid", fmt.Sprintf("the password needs at least %d characters", minPassword))
		return
	}
	o := engine.Owner{Username: strings.TrimSpace(b.Username), PasswordHash: hashPassword(b.Password), UserHandle: engine.NewUserHandle()}
	options, session, err := s.webauthn.BeginRegistration(user{owner: o})
	if err != nil {
		failEngine(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ceremony{Flow: s.flows.put(flow{kind: "setup", session: *session, owner: o}), Mode: "create", Options: options})
}

// loginStart checks the username and password, then asks for a passkey, or
// to enroll one when the account has none.
func (s *Server) loginStart(w http.ResponseWriter, r *http.Request) {
	if !browserOnly(w, r) {
		return
	}
	if s.limit.blocked() {
		fail(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; wait a minute")
		return
	}
	var b credentialsBody
	if !decode(w, r, &b) {
		return
	}
	u, _, err := s.ownerUser(r)
	if errors.Is(err, engine.ErrNotFound) {
		fail(w, http.StatusConflict, "no_owner", "create the owner account first")
		return
	}
	if err != nil {
		failEngine(w, err)
		return
	}
	// Both are checked, always, so the answer does not tell which failed.
	nameOK := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(b.Username)), []byte(u.owner.Username)) == 1
	if !checkPassword(u.owner.PasswordHash, b.Password) || !nameOK {
		s.limit.fail()
		fail(w, http.StatusUnauthorized, "unauthorized", "wrong username or password")
		return
	}
	if len(u.creds) == 0 {
		options, session, err := s.webauthn.BeginRegistration(u)
		if err != nil {
			failEngine(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ceremony{Flow: s.flows.put(flow{kind: "enroll", session: *session}), Mode: "create", Options: options})
		return
	}
	options, session, err := s.webauthn.BeginLogin(u)
	if err != nil {
		failEngine(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ceremony{Flow: s.flows.put(flow{kind: "login", session: *session}), Mode: "get", Options: options})
}

// finish completes a ceremony; the browser posts the credential as the body.
func (s *Server) finish(w http.ResponseWriter, r *http.Request) {
	if !browserOnly(w, r) {
		return
	}
	fl, ok := s.flows.take(r.URL.Query().Get("flow"))
	if !ok || fl.kind == "add" {
		fail(w, http.StatusBadRequest, "invalid", "unknown or expired sign-in; start again")
		return
	}
	name := passkeyName(r)
	switch fl.kind {
	case "setup":
		cred, err := s.webauthn.FinishRegistration(user{owner: fl.owner}, fl.session, r)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid", "the passkey was not accepted: "+err.Error())
			return
		}
		key, err := passkeyOf(cred, name)
		if err == nil {
			err = s.engine.CreateOwner(r.Context(), fl.owner, &key)
		}
		if err != nil {
			failEngine(w, err)
			return
		}
	case "enroll":
		u, _, err := s.ownerUser(r)
		if err != nil {
			failEngine(w, err)
			return
		}
		cred, err := s.webauthn.FinishRegistration(u, fl.session, r)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid", "the passkey was not accepted: "+err.Error())
			return
		}
		key, err := passkeyOf(cred, name)
		if err == nil {
			err = s.engine.AddPasskey(r.Context(), key)
		}
		if err != nil {
			failEngine(w, err)
			return
		}
	case "login":
		u, _, err := s.ownerUser(r)
		if err != nil {
			failEngine(w, err)
			return
		}
		cred, err := s.webauthn.FinishLogin(u, fl.session, r)
		if err != nil {
			s.limit.fail()
			fail(w, http.StatusUnauthorized, "unauthorized", "the passkey was not accepted")
			return
		}
		key, err := passkeyOf(cred, "")
		if err == nil {
			err = s.engine.UsePasskey(r.Context(), key.ID, key.Credential)
		}
		if err != nil {
			failEngine(w, err)
			return
		}
	}
	s.startSession(w, r)
}

func passkeyName(r *http.Request) string {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if len(name) > 60 {
		name = name[:60]
	}
	return name
}

func passkeyOf(c *webauthn.Credential, name string) (engine.Passkey, error) {
	b, err := json.Marshal(c)
	return engine.Passkey{ID: base64.RawURLEncoding.EncodeToString(c.ID), Name: name, Credential: b}, err
}

// Passkeys of a signed-in owner.

func (s *Server) passkeys(w http.ResponseWriter, r *http.Request, p principal) {
	keys, err := s.engine.Passkeys(r.Context())
	respond(w, map[string]any{"passkeys": keys}, err)
}

func (s *Server) addPasskeyStart(w http.ResponseWriter, r *http.Request, p principal) {
	if !p.browser {
		fail(w, http.StatusForbidden, "forbidden", "passkeys are added from the owner's browser session")
		return
	}
	u, _, err := s.ownerUser(r)
	if err != nil {
		failEngine(w, err)
		return
	}
	options, session, err := s.webauthn.BeginRegistration(u, webauthn.WithExclusions(webauthn.Credentials(u.creds).CredentialDescriptors()))
	if err != nil {
		failEngine(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ceremony{Flow: s.flows.put(flow{kind: "add", session: *session}), Mode: "create", Options: options})
}

func (s *Server) addPasskeyFinish(w http.ResponseWriter, r *http.Request, p principal) {
	fl, ok := s.flows.take(r.URL.Query().Get("flow"))
	if !ok || fl.kind != "add" || !p.browser {
		fail(w, http.StatusBadRequest, "invalid", "unknown or expired registration; start again")
		return
	}
	u, _, err := s.ownerUser(r)
	if err != nil {
		failEngine(w, err)
		return
	}
	cred, err := s.webauthn.FinishRegistration(u, fl.session, r)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid", "the passkey was not accepted: "+err.Error())
		return
	}
	key, err := passkeyOf(cred, passkeyName(r))
	if err == nil {
		err = s.engine.AddPasskey(r.Context(), key)
	}
	respond(w, map[string]bool{"added": err == nil}, err)
}

func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request, p principal) {
	if !p.browser {
		fail(w, http.StatusForbidden, "forbidden", "passkeys are removed from the owner's browser session")
		return
	}
	err := s.engine.DeletePasskey(r.Context(), r.PathValue("id"))
	respond(w, map[string]bool{"deleted": err == nil}, err)
}
