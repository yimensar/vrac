package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yan-imensar/vrac/internal/engine"
)

type harness struct {
	t      *testing.T
	url    string
	srv    *httptest.Server
	web    *http.Client
	engine *engine.Engine
}

func newHarness(t *testing.T) *harness {
	return newHarnessWith(t, nil)
}

func newHarnessWith(t *testing.T, web fs.FS) *harness {
	return newHarnessOpts(t, func(o *Options) { o.Web = web })
}

func newHarnessOpts(t *testing.T, configure func(*Options)) *harness {
	t.Helper()
	paris, _ := time.LoadLocation("Europe/Paris")
	e, err := engine.Open(filepath.Join(t.TempDir(), "vrac.sqlite"), engine.Options{Location: paris})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	h := &harness{t: t, engine: e}
	var handler http.Handler
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(h.srv.Close)
	// Passkeys need a domain: the instance is reached as localhost.
	h.url = strings.Replace(h.srv.URL, "127.0.0.1", "localhost", 1)
	opts := Options{PublicURL: h.url, Version: "test", Timezone: "Europe/Paris"}
	configure(&opts)
	s, err := New(e, opts)
	if err != nil {
		t.Fatal(err)
	}
	handler = s
	jar, _ := cookiejar.New(nil)
	h.web = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return h
}

// call sends JSON as the browser does and decodes the response.
func (h *harness) call(method, path string, body any, want int) map[string]any {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.url+path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(clientHeader, "web")
	res, err := h.web.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != want {
		h.t.Fatalf("%s %s = %d, want %d: %v", method, path, res.StatusCode, want, out)
	}
	return out
}

func TestOwnerSetupSessionsAndCSRF(t *testing.T) {
	h := newHarness(t)
	h.call("GET", "/api/v1/today", nil, http.StatusUnauthorized)
	if s := h.call("GET", "/api/v1/session", nil, 200); s["owner"] != false {
		t.Fatalf("session = %v", s)
	}
	h.ceremony("/api/v1/setup/start", map[string]string{"username": "yan", "password": "short"}, http.StatusBadRequest)
	h.ceremony("/api/v1/setup/start", map[string]string{"username": "with space", "password": password}, http.StatusBadRequest)
	a := h.setup()
	h.ceremony("/api/v1/setup/start", map[string]string{"username": "eve", "password": "another long one"}, http.StatusConflict)
	h.call("GET", "/api/v1/today", nil, 200)

	// A cross-site form cannot add the client header.
	req, _ := http.NewRequest("POST", h.url+"/api/v1/nodes", strings.NewReader(`{}`))
	res, _ := h.web.Do(req)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("mutation without client header = %d", res.StatusCode)
	}

	h.call("POST", "/api/v1/logout", nil, 200)
	h.call("GET", "/api/v1/today", nil, http.StatusUnauthorized)
	// The password alone opens nothing: it starts a passkey ceremony.
	h.ceremony("/api/v1/login/start", map[string]string{"username": "yan", "password": "wrong password"}, http.StatusUnauthorized)
	h.ceremony("/api/v1/login/start", map[string]string{"username": "nobody", "password": password}, http.StatusUnauthorized)
	c := h.ceremony("/api/v1/login/start", map[string]string{"username": "yan", "password": password}, 200)
	if c.Mode != "get" {
		t.Fatalf("mode = %q", c.Mode)
	}
	h.call("GET", "/api/v1/today", nil, http.StatusUnauthorized)
	// Another key cannot answer for the owner's passkey.
	h.answer(newAuthenticator(t), c, "/api/v1/signin/finish", http.StatusUnauthorized)
	c = h.ceremony("/api/v1/login/start", map[string]string{"username": "yan", "password": password}, 200)
	h.answer(a, c, "/api/v1/signin/finish", 200)
	h.call("GET", "/api/v1/today", nil, 200)
	// A flow is used once.
	h.answer(a, c, "/api/v1/signin/finish", http.StatusBadRequest)
}

func TestPasskeyEnrollmentAndManagement(t *testing.T) {
	h := newHarness(t)
	a := h.setup()
	second := newAuthenticator(t)
	c := h.ceremony("/api/v1/passkeys/start", nil, 200)
	h.answer(second, c, "/api/v1/passkeys/finish", 200)
	keys := h.call("GET", "/api/v1/passkeys", nil, 200)["passkeys"].([]any)
	if len(keys) != 2 {
		t.Fatalf("passkeys = %v", keys)
	}
	// Either passkey signs in.
	h.call("POST", "/api/v1/logout", nil, 200)
	h.signIn(second)
	first := keys[0].(map[string]any)["id"].(string)
	h.call("DELETE", "/api/v1/passkeys/"+first, nil, 200)
	h.call("DELETE", "/api/v1/passkeys/"+keys[1].(map[string]any)["id"].(string), nil, http.StatusConflict)
	_ = a

	// Recovery: without passkeys, the password leads to enrolling one.
	if _, err := h.engine.ResetPasskeys(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.call("GET", "/api/v1/today", nil, http.StatusUnauthorized)
	fresh := newAuthenticator(t)
	c = h.ceremony("/api/v1/login/start", map[string]string{"username": "yan", "password": password}, 200)
	if c.Mode != "create" {
		t.Fatalf("mode after reset = %q", c.Mode)
	}
	h.answer(fresh, c, "/api/v1/signin/finish", 200)
	h.call("POST", "/api/v1/logout", nil, 200)
	h.signIn(fresh)
}

func TestAPIWritesAndUnresolvedNames(t *testing.T) {
	h := newHarness(t)
	h.setup()
	out := h.call("POST", "/api/v1/nodes", map[string]any{"request_id": "1", "nodes": []any{map[string]string{"input": "Call [[Northwind]] #task"}}}, http.StatusUnprocessableEntity)
	if out["error"] != "unresolved" {
		t.Fatalf("out = %v", out)
	}
	// Tags are the user's: task becomes a to-do tag, project a plain one.
	h.call("POST", "/api/v1/types", map[string]any{"request_id": "t1", "name": "task", "todo": true}, 200)
	out = h.call("POST", "/api/v1/nodes", map[string]any{"request_id": "2", "nodes": []any{map[string]string{"input": "Call [[Northwind]] #task"}},
		"create": []any{map[string]any{"name": "Northwind", "types": []string{"project"}}}, "create_types": []string{"project"}}, 200)
	task := out["nodes"].([]any)[0].(map[string]any)
	if task["status"] != "open" {
		t.Fatalf("a to-do tag opens a checkbox: %v", task)
	}
	today := h.call("GET", "/api/v1/today", nil, 200)
	if open := today["open"].(map[string]any)["nodes"].([]any); len(open) != 1 || len(today["overdue"].(map[string]any)["nodes"].([]any)) != 0 {
		t.Fatalf("today = %v", today)
	}
	if p := h.call("GET", "/api/v1/types/project", nil, 200); p["count"].(float64) != 1 || p["todo"] != false {
		t.Fatalf("project tag = %v", p)
	}
	northwind := out["created"].([]any)[0].(map[string]any)["id"].(string)
	lists := h.call("GET", "/api/v1/nodes/"+northwind+"/lists", nil, 200)["lists"].([]any)
	if len(lists) != 1 || lists[0].(map[string]any)["type"] != "task" {
		t.Fatalf("lists = %v", lists)
	}
	if p := h.call("PATCH", "/api/v1/types/project", map[string]any{"todo": true}, 200); p["todo"] != true {
		t.Fatalf("trait = %v", p)
	}
}

// TestOAuthAndMCP connects an MCP client the way Claude or ChatGPT does:
// discovery, registration, owner approval, PKCE token exchange, then tools.
func TestOAuthAndMCP(t *testing.T) {
	h := newHarness(t)
	h.setup()

	res, _ := http.Post(h.url+"/mcp", "application/json", strings.NewReader(`{}`))
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(res.Header.Get("WWW-Authenticate"), "oauth-protected-resource") {
		t.Fatalf("unauthenticated MCP = %d %q", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}
	var meta map[string]any
	r, _ := http.Get(h.url + "/.well-known/oauth-authorization-server")
	json.NewDecoder(r.Body).Decode(&meta)
	if meta["issuer"] != h.url {
		t.Fatalf("metadata = %v", meta)
	}

	reg, _ := http.Post(h.url+"/oauth/register", "application/json", strings.NewReader(`{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`))
	var client map[string]any
	json.NewDecoder(reg.Body).Decode(&client)
	id, _ := client["client_id"].(string)
	if reg.StatusCode != http.StatusCreated || id == "" {
		t.Fatalf("registration = %d %v", reg.StatusCode, client)
	}
	bad, _ := http.Post(h.url+"/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["http://evil.example/cb"]}`))
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-loopback http redirect accepted: %d", bad.StatusCode)
	}

	verifier := "a-very-long-random-verifier-string-for-pkce-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {id}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "state": {"xyz"}, "scope": {"vrac"}}
	auth, _ := h.web.Get(h.url + "/oauth/authorize?" + q.Encode())
	if auth.StatusCode != http.StatusFound || !strings.HasPrefix(auth.Header.Get("Location"), "/authorize?") {
		t.Fatalf("authorize = %d %s", auth.StatusCode, auth.Header.Get("Location"))
	}
	if info := h.call("GET", "/api/v1/oauth/request?"+q.Encode(), nil, 200); info["client_name"] != "Claude" {
		t.Fatalf("consent info = %v", info)
	}
	decision := h.call("POST", "/api/v1/oauth/decision", map[string]any{"query": q.Encode(), "approve": true}, 200)
	back, _ := url.Parse(decision["redirect"].(string))
	if back.Query().Get("state") != "xyz" || back.Query().Get("code") == "" {
		t.Fatalf("redirect = %s", back)
	}
	exchange := func(form url.Values) (int, map[string]any) {
		res, _ := http.PostForm(h.url+"/oauth/token", form)
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {back.Query().Get("code")},
		"redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "code_verifier": {"wrong"}}
	if status, _ := exchange(form); status != http.StatusBadRequest {
		t.Fatalf("wrong verifier accepted: %d", status)
	}
	// The code was consumed by the failed attempt: get a new one.
	decision = h.call("POST", "/api/v1/oauth/decision", map[string]any{"query": q.Encode(), "approve": true}, 200)
	back, _ = url.Parse(decision["redirect"].(string))
	form.Set("code", back.Query().Get("code"))
	form.Set("code_verifier", verifier)
	status, tokens := exchange(form)
	if status != 200 || tokens["access_token"] == nil {
		t.Fatalf("token = %d %v", status, tokens)
	}
	status, refreshed := exchange(url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {tokens["refresh_token"].(string)}})
	if status != 200 {
		t.Fatalf("refresh = %d %v", status, refreshed)
	}
	if status, _ := exchange(url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {tokens["refresh_token"].(string)}}); status != http.StatusBadRequest {
		t.Fatalf("refresh token reuse accepted: %d", status)
	}

	ctx := context.Background()
	transport := &mcp.StreamableClientTransport{Endpoint: h.url + "/mcp", HTTPClient: &http.Client{Transport: bearer{refreshed["access_token"].(string)}}}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !strings.Contains(session.InitializeResult().Instructions, "memory") {
		t.Fatal("server instructions missing")
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 12 {
		t.Fatalf("tools = %d, %v", len(tools.Tools), err)
	}
	call := func(name string, args any) *mcp.CallToolResult {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	rejected := call("vrac_write", map[string]any{"request_id": "w1", "source": "outlook", "nodes": []any{map[string]any{"input": "Send the estimate to [[Marc]] #task"}}})
	if !rejected.IsError || !strings.Contains(rejected.Content[0].(*mcp.TextContent).Text, "Marc") {
		t.Fatalf("unknown name accepted: %+v", rejected)
	}
	if r := call("vrac_create_type", map[string]any{"request_id": "t1", "source": "chat", "name": "task", "todo": true}); r.IsError {
		t.Fatalf("create type = %+v", r.Content[0])
	}
	written := call("vrac_write", map[string]any{"request_id": "w2", "source": "outlook",
		"nodes":        []any{map[string]any{"input": "Copil [[Marc]] #meeting", "date_text": "14h", "children": []any{map[string]any{"input": "Send the estimate #task", "date_text": "Friday"}}}},
		"create":       []any{map[string]any{"name": "Marc", "types": []string{"person"}}},
		"create_types": []string{"meeting", "person"}})
	if written.IsError {
		t.Fatalf("write = %+v", written.Content[0])
	}
	if nodes := written.StructuredContent.(map[string]any)["nodes"].([]any); !strings.HasSuffix(nodes[0].(map[string]any)["date"].(string), "T14:00") || nodes[1].(map[string]any)["status"] != "open" {
		t.Fatalf("written = %v", nodes)
	}
	got := call("vrac_search", map[string]any{"types": []string{"task"}, "concerns": []string{written.StructuredContent.(map[string]any)["created"].([]any)[0].(map[string]any)["id"].(string)}})
	nodes := got.StructuredContent.(map[string]any)["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["updated_by"] != "Claude" || nodes[0].(map[string]any)["source"] != "outlook" {
		t.Fatalf("search = %v", got.StructuredContent)
	}
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestServesTheSinglePageApp(t *testing.T) {
	web := fstest.MapFS{
		"index.html":           {Data: []byte("<html>vrac</html>")},
		"assets/app.js":        {Data: []byte("console.log(1)")},
		"manifest.webmanifest": {Data: []byte("{}")},
	}
	h := newHarnessWith(t, web)
	for path, want := range map[string]string{"/": "text/html", "/node/abc": "text/html", "/assets/app.js": "javascript", "/manifest.webmanifest": "application/manifest+json"} {
		res, err := http.Get(h.url + path)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), want) {
			t.Errorf("%s = %d %s", path, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
	if res, _ := http.Get(h.url + "/api/v1/nope"); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown API path = %d", res.StatusCode)
	}
	if res, _ := http.Post(h.url+"/somewhere", "text/plain", nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST to the app = %d", res.StatusCode)
	}
}

func TestResetPassword(t *testing.T) {
	h := newHarness(t)
	auth := h.setup()
	e := h.engine
	if err := ResetPassword(context.Background(), e, "", "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := ResetPassword(context.Background(), e, "", "a brand new password"); err != nil {
		t.Fatal(err)
	}
	h.call("GET", "/api/v1/today", nil, http.StatusUnauthorized)
	h.ceremony("/api/v1/login/start", map[string]string{"username": "yan", "password": password}, http.StatusUnauthorized)
	c := h.ceremony("/api/v1/login/start", map[string]string{"username": "yan", "password": "a brand new password"}, 200)
	h.answer(auth, c, "/api/v1/signin/finish", 200)
	h.call("GET", "/api/v1/today", nil, 200)
}

// Without an owner, the command line creates one, who enrolls a passkey.
func TestResetPasswordCreatesOwner(t *testing.T) {
	h := newHarness(t)
	if err := ResetPassword(context.Background(), h.engine, "", password); err == nil {
		t.Fatal("an owner without a username was created")
	}
	if err := ResetPassword(context.Background(), h.engine, "yan", password); err != nil {
		t.Fatal(err)
	}
	c := h.ceremony("/api/v1/login/start", map[string]string{"username": "yan", "password": password}, 200)
	if c.Mode != "create" {
		t.Fatalf("mode = %q", c.Mode)
	}
	h.answer(newAuthenticator(t), c, "/api/v1/signin/finish", 200)
	h.call("GET", "/api/v1/today", nil, 200)
}

func TestPreferencesAPI(t *testing.T) {
	h := newHarness(t)
	h.setup()
	if p := h.call("GET", "/api/v1/preferences", nil, 200); p["fold"] != "default" || p["scale"] != "default" {
		t.Fatalf("defaults = %v", p)
	}
	h.call("PUT", "/api/v1/preferences", map[string]any{"fold": "nope", "theme": "dark", "scale": "default"}, http.StatusBadRequest)
	h.call("PUT", "/api/v1/preferences", map[string]any{"fold": "collapsed", "theme": "dark", "scale": "large"}, 200)
	if p := h.call("GET", "/api/v1/preferences", nil, 200); p["fold"] != "collapsed" || p["theme"] != "dark" || p["scale"] != "large" {
		t.Fatalf("stored = %v", p)
	}
}
