package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// provider is a minimal OIDC provider: discovery, authorization with PKCE,
// tokens signed with RS256.
type provider struct {
	srv       *httptest.Server
	key       *rsa.PrivateKey
	subject   string
	email     string
	challenge map[string]string // code -> PKCE challenge
	nonce     map[string]string // code -> nonce
}

func newProvider(t *testing.T) *provider {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	p := &provider{key: key, subject: "user-1", email: "yan@example.com", challenge: map[string]string{}, nonce: map[string]string{}}
	mux := http.NewServeMux()
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/authorize",
			"token_endpoint": p.srv.URL + "/token", "jwks_uri": p.srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" {
			http.Error(w, "PKCE required", 400)
			return
		}
		code := "code-" + q.Get("state")[:8]
		p.challenge[code], p.nonce[code] = q.Get("code_challenge"), q.Get("nonce")
		http.Redirect(w, r, q.Get("redirect_uri")+"?code="+code+"&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		code := r.PostForm.Get("code")
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge[code] {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		claims, _ := json.Marshal(map[string]any{"iss": p.srv.URL, "sub": p.subject, "aud": "vrac-web", "exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(), "nonce": p.nonce[code], "email": p.email, "email_verified": true})
		obj, _ := signer.Sign(claims)
		token, _ := obj.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": token})
	})
	return p
}

// follow runs redirects by hand, so cookies and hosts behave as in a
// browser, and returns the last response.
func follow(t *testing.T, c *http.Client, start string) *http.Response {
	t.Helper()
	next := start
	for range 10 {
		res, err := c.Get(next)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusFound {
			return res
		}
		loc, _ := res.Request.URL.Parse(res.Header.Get("Location"))
		// Follow the sign-in through the provider; stop once Vrac sends the
		// browser back to one of its pages.
		if loc.Host == res.Request.URL.Host && !strings.HasPrefix(loc.Path, "/auth/oidc/") {
			return res
		}
		next = loc.String()
	}
	t.Fatal("too many redirects")
	return nil
}

func TestOIDCSignIn(t *testing.T) {
	p := newProvider(t)
	h := newHarnessOpts(t, func(o *Options) {
		o.OIDC = &OIDC{Issuer: p.srv.URL, ClientID: "vrac-web", Name: "Pocket ID", Allowed: []string{"yan@example.com"}}
	})
	if s := h.call("GET", "/api/v1/session", nil, 200); s["provider"] != "Pocket ID" {
		t.Fatalf("session = %v", s)
	}
	res := follow(t, h.web, h.url+"/auth/oidc/start?return="+url.QueryEscape("/authorize?client_id=x"))
	if res.StatusCode != http.StatusFound || !strings.HasPrefix(res.Header.Get("Location"), "/authorize?client_id=x") {
		t.Fatalf("callback = %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	h.call("GET", "/api/v1/today", nil, 200)

	// A callback that did not start in this browser is refused.
	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if res, _ := other.Get(h.url + "/auth/oidc/callback?code=x&state=forged"); res.StatusCode != http.StatusForbidden {
		t.Fatalf("forged callback = %d", res.StatusCode)
	}
	// An identity outside the allowed list is refused.
	p.email, p.subject = "eve@example.com", "user-2"
	jar2, _ := cookiejar.New(nil)
	eve := &http.Client{Jar: jar2, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if res := follow(t, eve, h.url+"/auth/oidc/start"); res.StatusCode != http.StatusForbidden {
		t.Fatalf("other identity = %d", res.StatusCode)
	}
	// Returns stay on this site.
	if safeReturn("//evil.example") != "/" || safeReturn("https://evil.example") != "/" || safeReturn("/node/x") != "/node/x" {
		t.Fatal("unsafe return accepted")
	}
}
