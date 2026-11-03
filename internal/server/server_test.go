package server_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yimensar/vrac/internal/server"
	"github.com/yimensar/vrac/internal/store"
	"golang.org/x/oauth2"
)

const password = "synthetic-test-password-only"

type fixture struct {
	s      *store.Store
	ts     *httptest.Server
	client *http.Client
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(nil)
	base := "http://" + ts.Listener.Addr().String()
	srv, err := server.New(s, server.Config{PublicURL: base, WebDir: "", AttachmentDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = srv.Handler
	ts.Start()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(func() { ts.Close(); s.Close() })
	return &fixture{s, ts, client}
}
func (f *fixture) json(t *testing.T, method, path string, in any, status int) map[string]any {
	t.Helper()
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, f.ts.URL+path, body)
	if in != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != status {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, path, status, resp.StatusCode, raw)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
func (f *fixture) setup(t *testing.T) {
	f.json(t, "POST", "/api/setup", map[string]any{"password": password}, 200)
}
func TestAPIAuthenticationVersioningAndIsolation(t *testing.T) {
	f := newFixture(t)
	f.json(t, "GET", "/api/notes", nil, 401)
	session := f.json(t, "GET", "/api/session", nil, 200)
	if session["needsSetup"] != true {
		t.Fatal("fresh instance must need owner setup")
	}
	f.json(t, "POST", "/api/setup", map[string]string{"password": "short"}, 400)
	f.setup(t)
	f.json(t, "POST", "/api/setup", map[string]string{"password": password}, 409)
	n := f.json(t, "POST", "/api/notes", map[string]any{"content": "# JWT deployment\n\nUse JWT for Salesforce. #salesforce", "idempotencyKey": "test-create-key"}, 201)
	id := n["id"].(string)
	same := f.json(t, "POST", "/api/notes", map[string]any{"content": "# JWT deployment\n\nUse JWT for Salesforce. #salesforce", "idempotencyKey": "test-create-key"}, 201)
	if same["id"] != id {
		t.Fatal("retry created duplicate")
	}
	f.json(t, "PATCH", "/api/notes/"+id, map[string]any{"content": "# JWT deployment\n\nRevised content. #salesforce", "version": n["version"]}, 200)
	f.json(t, "PATCH", "/api/notes/"+id, map[string]any{"content": "stale overwrite", "version": n["version"]}, 409)
	page := f.json(t, "GET", "/api/notes?q=JWT", nil, 200)
	if page["total"].(float64) != 1 {
		t.Fatal("search did not find note")
	}
	f.json(t, "GET", "/api/export", nil, 200)
	req, _ := http.NewRequest("POST", f.ts.URL+"/api/notes", strings.NewReader(`{"content":"cross-origin"}`))
	req.Header.Set("Origin", "https://attacker.invalid")
	req.Header.Set("Content-Type", "application/json")
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("cross-origin mutation allowed: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("GET", f.ts.URL+"/api/notes", nil)
	req.Host = "attacker.invalid"
	res, err = f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 421 {
		t.Fatalf("host mismatch allowed: %d", res.StatusCode)
	}
	f.json(t, "POST", "/api/logout", map[string]any{}, 200)
	f.json(t, "GET", "/api/notes", nil, 401)
	f.json(t, "POST", "/api/login", map[string]string{"password": password}, 200)
	f.json(t, "GET", "/api/notes", nil, 200)
}
func register(t *testing.T, f *fixture) string {
	t.Helper()
	out := f.json(t, "POST", "/oauth/register", map[string]any{"client_name": "Integration test", "redirect_uris": []string{"http://127.0.0.1:9999/callback"}, "token_endpoint_auth_method": "none"}, 201)
	return out["client_id"].(string)
}
func authorize(t *testing.T, f *fixture, clientID, scope string) (string, string) {
	t.Helper()
	verifier := strings.Repeat("x", 64)
	sha := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:9999/callback"}, "response_type": {"code"}, "scope": {scope}, "state": {"test-state"}, "resource": {f.ts.URL + "/mcp"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sha[:])}, "code_challenge_method": {"S256"}}
	endpoint := f.ts.URL + "/oauth/authorize?" + q.Encode()
	resp, err := f.client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("authorize page: %d %s", resp.StatusCode, raw)
	}
	match := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindSubmatch(raw)
	if len(match) != 2 {
		t.Fatal("missing consent CSRF token")
	}
	badConsent, err := f.client.PostForm(endpoint, url.Values{"csrf": {"wrong-token"}, "decision": {"approve"}})
	if err != nil {
		t.Fatal(err)
	}
	badConsent.Body.Close()
	if badConsent.StatusCode != http.StatusForbidden {
		t.Fatal("consent accepted an invalid CSRF token")
	}
	form := url.Values{"csrf": {string(match[1])}, "decision": {"approve"}}
	resp, err = f.client.PostForm(endpoint, form)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("consent: %d %s", resp.StatusCode, raw)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Query().Get("state") != "test-state" {
		t.Fatal("lost OAuth state")
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatalf("no code: %s", location)
	}
	return code, verifier
}
func token(t *testing.T, f *fixture, form url.Values, status int) map[string]any {
	t.Helper()
	resp, err := f.client.PostForm(f.ts.URL+"/oauth/token", form)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("token status %d want %d: %s", resp.StatusCode, status, raw)
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		t.Fatalf("token response not JSON: %s", raw)
	}
	return out
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(clone)
}
func connectMCP(t *testing.T, f *fixture, access string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "vrac-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: f.ts.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{access}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func TestOAuthPKCERotationRevocationAndMCPScopes(t *testing.T) {
	f := newFixture(t)
	f.setup(t)
	cid := register(t, f)
	f.json(t, "POST", "/oauth/register", map[string]any{"redirect_uris": []string{"http://attacker.invalid/callback"}}, 400)
	code, verifier := authorize(t, f, cid, "notes:read")
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {cid}, "redirect_uri": {"http://127.0.0.1:9999/callback"}, "code": {code}, "code_verifier": {verifier}, "resource": {f.ts.URL + "/mcp"}}
	bad, _ := url.ParseQuery(form.Encode())
	bad.Set("code_verifier", strings.Repeat("z", 64))
	token(t, f, bad, 400)
	// A failed verifier consumes the code. Start another explicit authorization.
	code, verifier = authorize(t, f, cid, "notes:read")
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	issued := token(t, f, form, 200)
	access := issued["access_token"].(string)
	refresh := issued["refresh_token"].(string)
	token(t, f, form, 400)
	otherClient := register(t, f)
	token(t, f, url.Values{"grant_type": {"refresh_token"}, "client_id": {otherClient}, "refresh_token": {refresh}, "resource": {f.ts.URL + "/mcp"}}, 400)
	token(t, f, url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {refresh}, "resource": {"https://wrong.invalid/mcp"}}, 400)
	token(t, f, url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {refresh}, "resource": {f.ts.URL + "/mcp"}, "scope": {"notes:read notes:write"}}, 400)
	mcpSession := connectMCP(t, f, access)
	list, err := mcpSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(list.Tools))
	}
	result, err := mcpSession.CallTool(context.Background(), &sdk.CallToolParams{Name: "create_note", Arguments: map[string]any{"content": "Must not write"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("read-only token wrote note")
	}
	rotated := token(t, f, url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {refresh}, "resource": {f.ts.URL + "/mcp"}}, 200)
	token(t, f, url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {refresh}, "resource": {f.ts.URL + "/mcp"}}, 400)
	req, _ := http.NewRequest("POST", f.ts.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+access)
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("old access token survived rotation: %d", res.StatusCode)
	}
	refreshed := rotated["access_token"].(string)
	res, err = f.client.PostForm(f.ts.URL+"/oauth/revoke", url.Values{"client_id": {cid}, "token": {refreshed}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	req, _ = http.NewRequest("POST", f.ts.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+refreshed)
	res, err = f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("revoked token still authorized")
	}
	code, verifier = authorize(t, f, cid, "notes:read notes:write")
	issued = token(t, f, url.Values{"grant_type": {"authorization_code"}, "client_id": {cid}, "redirect_uri": {"http://127.0.0.1:9999/callback"}, "code": {code}, "code_verifier": {verifier}, "resource": {f.ts.URL + "/mcp"}}, 200)
	writeSession := connectMCP(t, f, issued["access_token"].(string))
	result, err = writeSession.CallTool(context.Background(), &sdk.CallToolParams{Name: "create_note", Arguments: map[string]any{"content": "# OAuth integration\n\nA persistent MCP note. #testing", "idempotencyKey": "oauth-test-create"}})
	if err != nil || result.IsError {
		t.Fatalf("MCP write failed: %v %+v", err, result)
	}
	result, err = writeSession.CallTool(context.Background(), &sdk.CallToolParams{Name: "search_notes", Arguments: map[string]any{"query": "OAuth"}})
	if err != nil || result.IsError {
		t.Fatalf("MCP search failed: %v %+v", err, result)
	}
}

// This uses the real OAuth client refresh path, rather than a hand-built form:
// golang.org/x/oauth2 omits resource when refreshing an expired access token.
func TestOAuthTokenSourceRefreshWithoutResource(t *testing.T) {
	f := newFixture(t)
	f.setup(t)
	cid := register(t, f)
	code, verifier := authorize(t, f, cid, "notes:read")
	issued := token(t, f, url.Values{"grant_type": {"authorization_code"}, "client_id": {cid}, "redirect_uri": {"http://127.0.0.1:9999/callback"}, "code": {code}, "code_verifier": {verifier}, "resource": {f.ts.URL + "/mcp"}}, 200)
	config := oauth2.Config{ClientID: cid, Endpoint: oauth2.Endpoint{TokenURL: f.ts.URL + "/oauth/token", AuthStyle: oauth2.AuthStyleInParams}}
	expired := &oauth2.Token{AccessToken: issued["access_token"].(string), RefreshToken: issued["refresh_token"].(string), TokenType: "Bearer", Expiry: time.Now().Add(-time.Minute)}
	refreshed, err := config.TokenSource(context.Background(), expired).Token()
	if err != nil {
		t.Fatalf("standard TokenSource refresh failed: %v", err)
	}
	if refreshed.AccessToken == expired.AccessToken || refreshed.RefreshToken == expired.RefreshToken || !refreshed.Expiry.After(time.Now()) {
		t.Fatal("TokenSource did not rotate and renew credentials")
	}
	session := connectMCP(t, f, refreshed.AccessToken)
	if _, err = session.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("refreshed token could not use MCP: %v", err)
	}
	token(t, f, url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {refreshed.RefreshToken}, "resource": {"https://other.invalid/mcp"}}, 400)
	token(t, f, url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {expired.RefreshToken}}, 400)
}
