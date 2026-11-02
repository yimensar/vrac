package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"
)

func TestBrowserOIDCFlow(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "browser"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var issuer, nonce, challenge string
	exchanges := 0
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSON(w, 200, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}, "authorization_response_iss_parameter_supported": true})
		case "/keys":
			writeJSON(w, 200, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "browser", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			exchanges++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			_, _, basic := r.BasicAuth()
			if basic || r.Form.Has("client_secret") || r.Form.Get("client_id") != "web-client" || r.Form.Get("resource") != "https://vrac.example/mcp" || r.Form.Get("redirect_uri") != "https://vrac.example/auth/callback" || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != challenge {
				t.Error("invalid public-client exchange or PKCE binding")
				w.WriteHeader(400)
				return
			}
			kind := r.Form.Get("code")
			idClaims := map[string]any{"iss": issuer, "sub": "owner", "aud": "web-client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce}
			accessClaims := map[string]any{"iss": issuer, "sub": "owner", "aud": "https://vrac.example/mcp", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "scope": "vrac"}
			switch kind {
			case "nonce":
				idClaims["nonce"] = "wrong"
			case "id-audience":
				idClaims["aud"] = "other-client"
			case "access-audience":
				accessClaims["aud"] = "other-resource"
			case "scope":
				accessClaims["scope"] = "openid"
			case "expired-token":
				idClaims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "token-issuer":
				idClaims["iss"] = "https://other.example"
			case "subject":
				accessClaims["sub"] = "another-user"
			}
			sign := func(claims map[string]any) string {
				v, e := jwt.Signed(signer).Claims(claims).Serialize()
				if e != nil {
					t.Error(e)
				}
				return v
			}
			writeJSON(w, 200, map[string]any{"access_token": sign(accessClaims), "token_type": "Bearer", "expires_in": 3600, "id_token": sign(idClaims)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	issuer = provider.URL
	a, err := newAuth(oidc.ClientContext(context.Background(), provider.Client()), authOptions{Token: testToken, OAuthIssuer: issuer, OAuthAudience: "https://vrac.example/mcp", OAuthScope: "vrac", WebOIDCClientID: "web-client", WebOIDCProviderName: "Pocket ID"})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"valid", "nonce", "id-audience", "access-audience", "scope", "expired-token", "token-issuer", "subject", "missing-cookie", "wrong-cookie", "state", "missing-issuer", "issuer", "expired-flow", "provider-error"} {
		t.Run(kind, func(t *testing.T) {
			login := httptest.NewRecorder()
			a.browserLogin(login, httptest.NewRequest("GET", "https://vrac.example/auth/login", nil))
			if login.Code != 302 {
				t.Fatal(login.Body.String())
			}
			location, err := url.Parse(login.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			q := location.Query()
			nonce, challenge = q.Get("nonce"), q.Get("code_challenge")
			if q.Get("client_id") != "web-client" || q.Get("resource") != "https://vrac.example/mcp" || q.Get("code_challenge_method") != "S256" || q.Get("scope") != "openid vrac" || len(nonce) < 32 {
				t.Fatal("invalid authorization request")
			}
			cookie := login.Result().Cookies()[0]
			if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/auth" {
				t.Fatal("unsafe login cookie")
			}
			values := url.Values{"state": {q.Get("state")}, "code": {kind}, "iss": {issuer}}
			switch kind {
			case "wrong-cookie":
				cookie.Value = "unrelated-browser"
			case "state":
				values.Set("state", "wrong")
			case "issuer":
				values.Set("iss", "https://other.example")
			case "missing-issuer":
				values.Del("iss")
			case "provider-error":
				values.Set("error", "access_denied")
			case "expired-flow":
				a.mu.Lock()
				for k, f := range a.flows {
					f.expires = time.Now().Add(-time.Hour)
					a.flows[k] = f
				}
				a.mu.Unlock()
			}
			r := httptest.NewRequest("GET", "https://vrac.example/auth/callback?"+values.Encode(), nil)
			if kind != "missing-cookie" {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			a.browserCallback(w, r)
			var session *http.Cookie
			for _, c := range w.Result().Cookies() {
				if c.Name == sessionCookie {
					session = c
				}
			}
			if kind != "valid" {
				if session != nil || !strings.Contains(w.Header().Get("Location"), "auth_error=") {
					t.Fatal("invalid OIDC response created a session")
				}
				return
			}
			if session == nil || w.Header().Get("Location") != "/" || !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
				t.Fatal("missing secure session")
			}
			protected := httptest.NewRequest("GET", "https://vrac.example/api/tags", nil)
			protected.AddCookie(session)
			if !a.authorized(protected, true) || a.authorized(protected, false) {
				t.Fatal("session authorization or MCP isolation failed")
			}
			before := exchanges
			replay := httptest.NewRecorder()
			a.browserCallback(replay, r)
			if !strings.Contains(replay.Header().Get("Location"), "auth_error=") || exchanges != before {
				t.Fatal("callback replay accepted")
			}
		})
	}
}

func TestBrowserAuthConfiguration(t *testing.T) {
	_, ts := fixture(t)
	r := request(t, ts, "GET", "/auth/config", nil, false)
	status(t, r, 200)
	if decode[map[string]any](t, r)["enabled"] != false {
		t.Fatal("unexpected browser OIDC")
	}
	if _, err := newAuth(context.Background(), authOptions{Token: testToken, WebOIDCClientID: "web"}); err == nil {
		t.Fatal("OIDC client without issuer accepted")
	}
}
