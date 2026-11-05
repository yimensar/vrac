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
	srv    *httptest.Server
	web    *http.Client
	engine *engine.Engine
}

func newHarness(t *testing.T) *harness {
	return newHarnessWith(t, nil)
}

func newHarnessWith(t *testing.T, web fs.FS) *harness {
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
	s, err := New(e, Options{PublicURL: h.srv.URL, Version: "test", Timezone: "Europe/Paris", Web: web})
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
	req, _ := http.NewRequest(method, h.srv.URL+path, r)
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
	h.call("POST", "/api/v1/setup", map[string]string{"password": "short"}, http.StatusBadRequest)
	h.call("POST", "/api/v1/setup", map[string]string{"password": "correct horse battery"}, 200)
	h.call("POST", "/api/v1/setup", map[string]string{"password": "another long one"}, http.StatusConflict)
	h.call("GET", "/api/v1/today", nil, 200)

	// A cross-site form cannot add the client header.
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/nodes", strings.NewReader(`{}`))
	res, _ := h.web.Do(req)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("mutation without client header = %d", res.StatusCode)
	}

	h.call("POST", "/api/v1/logout", nil, 200)
	h.call("GET", "/api/v1/today", nil, http.StatusUnauthorized)
	h.call("POST", "/api/v1/login", map[string]string{"password": "wrong password"}, http.StatusUnauthorized)
	h.call("POST", "/api/v1/login", map[string]string{"password": "correct horse battery"}, 200)
}

func TestAPIWritesAndUnresolvedNames(t *testing.T) {
	h := newHarness(t)
	h.call("POST", "/api/v1/setup", map[string]string{"password": "correct horse battery"}, 200)
	out := h.call("POST", "/api/v1/nodes", map[string]any{"request_id": "1", "nodes": []any{map[string]string{"input": "Call [[Northwind]] #task"}}}, http.StatusUnprocessableEntity)
	if out["error"] != "unresolved" {
		t.Fatalf("out = %v", out)
	}
	h.call("POST", "/api/v1/nodes", map[string]any{"request_id": "2", "nodes": []any{map[string]string{"input": "Call [[Northwind]] #task"}},
		"create": []any{map[string]any{"name": "Northwind", "types": []string{"project"}}}}, 200)
	today := h.call("GET", "/api/v1/today", nil, 200)
	due := today["due"].(map[string]any)["nodes"].([]any)
	if len(due) != 0 {
		t.Fatalf("a task without due date is not due: %v", due)
	}
	types := h.call("GET", "/api/v1/types/project", nil, 200)
	if rows := types["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["open_tasks"].(float64) != 1 {
		t.Fatalf("project view = %v", types)
	}
}

// TestOAuthAndMCP connects an MCP client the way Claude or ChatGPT does:
// discovery, registration, owner approval, PKCE token exchange, then tools.
func TestOAuthAndMCP(t *testing.T) {
	h := newHarness(t)
	h.call("POST", "/api/v1/setup", map[string]string{"password": "correct horse battery"}, 200)

	res, _ := http.Post(h.srv.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(res.Header.Get("WWW-Authenticate"), "oauth-protected-resource") {
		t.Fatalf("unauthenticated MCP = %d %q", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}
	var meta map[string]any
	r, _ := http.Get(h.srv.URL + "/.well-known/oauth-authorization-server")
	json.NewDecoder(r.Body).Decode(&meta)
	if meta["issuer"] != h.srv.URL {
		t.Fatalf("metadata = %v", meta)
	}

	reg, _ := http.Post(h.srv.URL+"/oauth/register", "application/json", strings.NewReader(`{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`))
	var client map[string]any
	json.NewDecoder(reg.Body).Decode(&client)
	id, _ := client["client_id"].(string)
	if reg.StatusCode != http.StatusCreated || id == "" {
		t.Fatalf("registration = %d %v", reg.StatusCode, client)
	}
	bad, _ := http.Post(h.srv.URL+"/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["http://evil.example/cb"]}`))
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-loopback http redirect accepted: %d", bad.StatusCode)
	}

	verifier := "a-very-long-random-verifier-string-for-pkce-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {id}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "state": {"xyz"}, "scope": {"vrac"}}
	auth, _ := h.web.Get(h.srv.URL + "/oauth/authorize?" + q.Encode())
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
		res, _ := http.PostForm(h.srv.URL+"/oauth/token", form)
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
	transport := &mcp.StreamableClientTransport{Endpoint: h.srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{refreshed["access_token"].(string)}}}
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
	written := call("vrac_write", map[string]any{"request_id": "w2", "source": "outlook", "parse_dates": true,
		"nodes":  []any{map[string]any{"input": "Copil [[Marc]] 14h #meeting", "children": []any{map[string]any{"input": "Send the estimate Friday #task"}}}},
		"create": []any{map[string]any{"name": "Marc", "types": []string{"person"}}}})
	if written.IsError {
		t.Fatalf("write = %+v", written.Content[0])
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
		res, err := http.Get(h.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), want) {
			t.Errorf("%s = %d %s", path, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
	if res, _ := http.Get(h.srv.URL + "/api/v1/nope"); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown API path = %d", res.StatusCode)
	}
	if res, _ := http.Post(h.srv.URL+"/somewhere", "text/plain", nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST to the app = %d", res.StatusCode)
	}
}

func TestResetPassword(t *testing.T) {
	h := newHarness(t)
	h.call("POST", "/api/v1/setup", map[string]string{"password": "correct horse battery"}, 200)
	e := h.engine
	if err := ResetPassword(context.Background(), e, "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := ResetPassword(context.Background(), e, "a brand new password"); err != nil {
		t.Fatal(err)
	}
	h.call("GET", "/api/v1/today", nil, http.StatusUnauthorized)
	h.call("POST", "/api/v1/login", map[string]string{"password": "correct horse battery"}, http.StatusUnauthorized)
	h.call("POST", "/api/v1/login", map[string]string{"password": "a brand new password"}, 200)
}
