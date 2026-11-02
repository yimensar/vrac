package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yan-imensar/vrac/internal/engine"
)

const testToken = "a-test-token-long-enough-for-authentication-0123456789"

func fixture(t *testing.T) (*engine.Engine, *httptest.Server) {
	t.Helper()
	e, err := engine.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	s, err := New(context.Background(), e, Options{Token: testToken, Timezone: "Europe/Paris", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return e, ts
}

func request(t *testing.T, ts *httptest.Server, method, path string, body any, authorized bool) *http.Response {
	t.Helper()
	var data io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, ts.URL+path, data)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if authorized {
		r.Header.Set("Authorization", "Bearer "+testToken)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}
func decode[T any](t *testing.T, r *http.Response) T {
	t.Helper()
	var value T
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		t.Fatalf("decode status %d: %v", r.StatusCode, err)
	}
	return value
}
func status(t *testing.T, r *http.Response, want int) {
	t.Helper()
	if r.StatusCode != want {
		body, _ := io.ReadAll(r.Body)
		t.Fatalf("status %d, want %d: %s", r.StatusCode, want, body)
	}
}

func TestHTTPAuthenticationAndOrigin(t *testing.T) {
	_, ts := fixture(t)
	status(t, request(t, ts, "GET", "/healthz", nil, false), 200)
	status(t, request(t, ts, "GET", "/api/bootstrap", nil, false), 401)
	status(t, request(t, ts, "POST", "/mcp", map[string]string{}, false), 401)
	login := request(t, ts, "POST", "/api/session", map[string]string{"token": testToken}, false)
	status(t, login, 200)
	cookies := login.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie must be HttpOnly, Secure, SameSiteStrict")
	}
	r, _ := http.NewRequest("GET", ts.URL+"/api/bootstrap", nil)
	r.AddCookie(cookies[0])
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	status(t, resp, 200)
	r, _ = http.NewRequest("POST", ts.URL+"/api/nodes", strings.NewReader(`{"text":"should not be written"}`))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", "https://other.example")
	resp, err = http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	status(t, resp, 403)
	r, _ = http.NewRequest("DELETE", ts.URL+"/api/session", nil)
	r.AddCookie(cookies[0])
	resp, err = http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	status(t, resp, 200)
	r, _ = http.NewRequest("GET", ts.URL+"/api/tags", nil)
	r.AddCookie(cookies[0])
	resp, err = http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	status(t, resp, 401)
}

func TestDevelopmentRejectsDNSRebindingHost(t *testing.T) {
	e, _ := fixture(t)
	s, err := New(context.Background(), e, Options{Dev: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/healthz", "/api/bootstrap", "/mcp"} {
		r := httptest.NewRequest("GET", "http://evil.example"+target, nil)
		r.Header.Set("Origin", "http://evil.example")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("rebound Host accepted for %s: %d", target, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://localhost:8080/healthz", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("loopback Host rejected")
	}
}

func TestExportFailureIsNotSuccessfulDownload(t *testing.T) {
	e, ts := fixture(t)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	r := request(t, ts, "GET", "/api/export", nil, true)
	status(t, r, 500)
	if r.Header.Get("Content-Disposition") != "" {
		t.Fatal("failed export offered as download")
	}
}

func TestWebServesAssetsAndRejectsMissingFiles(t *testing.T) {
	e, _ := fixture(t)
	s, err := New(context.Background(), e, Options{Dev: true, Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Vrac</title>")}, "favicon.svg": {Data: []byte("<svg></svg>")}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path, content string
		code          int
	}{{"/", "text/html", 200}, {"/favicon.svg", "image/svg+xml", 200}, {"/missing.js", "", 404}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost:8080"+test.path, nil))
		if w.Code != test.code || (test.content != "" && !strings.Contains(w.Header().Get("Content-Type"), test.content)) {
			t.Fatalf("%s: %d %s", test.path, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

func TestHTTPJournalMutationSearchHistoryAndConflict(t *testing.T) {
	_, ts := fixture(t)
	r := request(t, ts, "GET", "/api/bootstrap?date=2026-11-02", nil, true)
	status(t, r, 200)
	boot := decode[struct {
		Today engine.Node `json:"today"`
	}](t, r)
	r = request(t, ts, "POST", "/api/capture", captureInput{Date: "2026-11-02", RequestID: "capture-test", Nodes: []engine.CreateInput{{Text: "Meeting [[Atlas]]", Tags: []string{"meeting"}, Children: []engine.CreateInput{{Text: "Use SSO", Tags: []string{"decision"}}}}}}, true)
	status(t, r, 201)
	captured := decode[struct {
		Nodes []engine.Node `json:"nodes"`
	}](t, r)
	meeting := captured.Nodes[0]
	if meeting.ParentID != boot.Today.ID {
		t.Fatal("capture did not target requested journal day")
	}
	r = request(t, ts, "GET", "/api/nodes/"+meeting.ID, nil, true)
	status(t, r, 200)
	detail := decode[struct {
		Children engine.NodePage `json:"children"`
	}](t, r)
	decision := detail.Children.Nodes[0]
	subject := meeting.References[0].TargetID
	r = request(t, ts, "GET", "/api/search?tag=decision&subject="+subject+"&from=2026-11-02&to=2026-11-02", nil, true)
	status(t, r, 200)
	page := decode[engine.SearchPage](t, r)
	if len(page.Items) != 1 || page.Items[0].Node.ID != decision.ID {
		t.Fatalf("inherited subject search: %#v", page)
	}
	in := engine.UpdateInput{Text: "Use SSO for staff", Tags: []string{"decision"}, ExpectedRevision: decision.Revision, Summary: "Clarify scope", RequestID: "update-decision"}
	r = request(t, ts, "PATCH", "/api/nodes/"+decision.ID, in, true)
	status(t, r, 200)
	updated := decode[struct {
		Node engine.Node `json:"node"`
	}](t, r)
	r = request(t, ts, "PATCH", "/api/nodes/"+decision.ID, in, true)
	status(t, r, 200) // safe network retry
	in.RequestID = "conflicting-edit"
	r = request(t, ts, "PATCH", "/api/nodes/"+decision.ID, in, true)
	status(t, r, 409)
	r = request(t, ts, "GET", "/api/search?tag=decision&subject="+subject, nil, true)
	status(t, r, 200)
	page = decode[engine.SearchPage](t, r)
	if len(page.Items) != 1 || page.Items[0].Node.Text != "Use SSO for staff" {
		t.Fatal("current search must contain one current decision")
	}
	r = request(t, ts, "GET", "/api/nodes/"+decision.ID+"/revisions", nil, true)
	status(t, r, 200)
	history := decode[struct {
		Revisions []struct {
			Revision int64  `json:"revision"`
			Text     string `json:"text"`
		}
	}](t, r)
	if len(history.Revisions) < 2 {
		t.Fatal("missing version history")
	}
	r = request(t, ts, "POST", "/api/nodes/"+decision.ID+"/restore", map[string]any{"revision": decision.Revision, "expected_revision": updated.Node.Revision, "request_id": "restore-decision"}, true)
	status(t, r, 200)
	r = request(t, ts, "GET", "/api/nodes/"+decision.ID, nil, true)
	status(t, r, 200)
	restored := decode[struct {
		Node engine.Node `json:"node"`
	}](t, r)
	if restored.Node.Text != decision.Text || restored.Node.Revision <= updated.Node.Revision {
		t.Fatal("restore must create a new current revision")
	}
	status(t, request(t, ts, "GET", "/api/search?limit=1000", nil, true), 400)
	status(t, request(t, ts, "POST", "/api/nodes", map[string]any{"text": "x", "unexpected": "field"}, true), 400)
}

type bearerTransport struct{ base http.RoundTripper }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+testToken)
	return b.base.RoundTrip(copy)
}

func TestMCPRealClientDiscoveryAndMutation(t *testing.T) {
	_, ts := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{http.DefaultTransport}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
		if tool.InputSchema == nil {
			t.Fatalf("missing input schema: %s", tool.Name)
		}
	}
	for _, name := range []string{"vrac_journal", "vrac_search", "vrac_read", "vrac_capture", "vrac_update", "vrac_restore", "vrac_revisions", "vrac_move", "vrac_backlinks", "vrac_tags", "vrac_children", "vrac_create"} {
		if !names[name] {
			t.Fatalf("missing MCP tool %s", name)
		}
	}
	args := map[string]any{"date": "2026-11-02", "request_id": "mcp-capture", "nodes": []map[string]any{{"text": "Meeting [[MCP project]]", "tags": []string{"meeting"}, "children": []map[string]any{{"text": "Keep one current decision", "tags": []string{"decision"}}}}}}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "vrac_capture", Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("capture failed: %v %#v", err, result)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "vrac_capture", Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("retry failed: %v %#v", err, result)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "vrac_search", Arguments: map[string]any{"tag": "decision", "from": "2026-11-02", "to": "2026-11-02"}})
	if err != nil || result.IsError {
		t.Fatalf("search failed: %v %#v", err, result)
	}
	data, _ := json.Marshal(result.StructuredContent)
	var found struct {
		Items []struct {
			Node engine.Node `json:"node"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &found); err != nil || len(found.Items) != 1 {
		t.Fatalf("expected exactly one decision, got %s", data)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "vrac_capture", Arguments: map[string]any{"nodes": []map[string]string{{"text": "missing retry key"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("mutation without request ID must fail usefully")
	}
}

func TestMCPPreviewsAndHistoricalRead(t *testing.T) {
	e, ts := fixture(t)
	subject, err := e.Create(engine.CreateInput{Text: strings.Repeat("é", 3000)})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := e.Create(engine.CreateInput{Text: "Preview parent", Children: []engine.CreateInput{{Text: strings.Repeat("🐈", 2000), Tags: []string{"preview"}, ReferenceIDs: []string{subject.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	children, err := e.Children(parent.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	child := children.Nodes[0]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "preview-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{http.DefaultTransport}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || r.IsError {
			t.Fatalf("%s: %v %#v", name, err, r)
		}
		b, _ := json.Marshal(r.StructuredContent)
		var result map[string]any
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	page := call("vrac_children", map[string]any{"id": parent.ID})
	n := page["nodes"].([]any)[0].(map[string]any)
	if n["text_truncated"] != true || n["reference_labels_truncated"] != true || len([]rune(n["text"].(string))) != 800 {
		t.Fatalf("unbounded child preview: %v", n)
	}
	refs := n["references"].([]any)
	label := refs[0].(map[string]any)["label"].(string)
	if len([]rune(label)) != 200 {
		t.Fatal("reference label not bounded")
	}
	history := call("vrac_revisions", map[string]any{"id": child.ID})
	old := history["revisions"].([]any)[0].(map[string]any)["node"].(map[string]any)
	if old["text_truncated"] != true {
		t.Fatal("revision preview not bounded")
	}
	full := call("vrac_read", map[string]any{"ids": []string{child.ID}, "revision": 1})
	read := full["items"].([]any)[0].(map[string]any)["node"].(map[string]any)
	if read["text"] != child.Text || read["text_truncated"] != false {
		t.Fatal("explicit revision read did not return complete text")
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "vrac_update" || tool.Name == "vrac_move" || tool.Name == "vrac_restore" {
			if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
				t.Fatalf("incorrect additive-only hint for %s", tool.Name)
			}
		}
		if tool.Name == "vrac_update" {
			schema := tool.InputSchema.(map[string]any)
			change := schema["properties"].(map[string]any)["change"].(map[string]any)
			found := false
			for _, key := range change["required"].([]any) {
				found = found || key == "request_id"
			}
			if !found {
				t.Fatal("retry key is optional in advertised schema")
			}
		}
	}
}

func TestOAuthVerificationAndDiscovery(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSON(w, 200, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			writeJSON(w, 200, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	issuer = provider.URL
	ctx := oidc.ClientContext(context.Background(), provider.Client())
	a, err := newAuth(ctx, authOptions{Token: testToken, OAuthIssuer: issuer, OAuthAudience: "https://vrac.example/mcp", OAuthScope: "vrac"})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "test"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	makeToken := func(audience, scope string, expires time.Time) string {
		value, err := jwt.Signed(signer).Claims(map[string]any{"iss": issuer, "sub": "owner", "aud": audience, "exp": expires.Unix(), "iat": time.Now().Unix(), "scope": scope}).Serialize()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, test := range []struct {
		name, aud, scope string
		expires          time.Time
		valid            bool
	}{{"valid", "https://vrac.example/mcp", "vrac", time.Now().Add(time.Hour), true}, {"wrong audience", "other", "vrac", time.Now().Add(time.Hour), false}, {"missing scope", "https://vrac.example/mcp", "other", time.Now().Add(time.Hour), false}, {"expired", "https://vrac.example/mcp", "vrac", time.Now().Add(-time.Hour), false}} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://vrac.example/mcp", nil)
			r.Header.Set("Authorization", "Bearer "+makeToken(test.aud, test.scope, test.expires))
			if a.authorized(r, false) != test.valid {
				t.Fatalf("authorization should be %v", test.valid)
			}
		})
	}
	r := httptest.NewRequest("GET", "https://vrac.example/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()
	a.metadata(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), issuer) {
		t.Fatal("missing protected resource discovery")
	}
	r = httptest.NewRequest("POST", "https://vrac.example/mcp", nil)
	w = httptest.NewRecorder()
	a.challenge(w, r)
	if !strings.Contains(w.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatal("missing OAuth discovery challenge")
	}
}
