package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/yimensar/vrac/internal/store"
)

func testAuth(t *testing.T) *Auth {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := New(s.DB(), Config{PublicURL: "http://127.0.0.1:5235"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestBearerExpiryAudienceAndHeaderOnly(t *testing.T) {
	a := testAuth(t)
	ctx := context.Background()
	token := models.NewToken()
	token.ClientID = "test"
	token.UserID = "owner"
	token.Access = "synthetic-access-token"
	token.Scope = "notes:read"
	token.AccessCreateAt = time.Now()
	token.AccessExpiresIn = time.Hour
	if err := a.storage.Create(ctx, token); err != nil {
		t.Fatal(err)
	}
	h := a.RequireMCP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !HasScope(r.Context(), "notes:read") {
			t.Error("scope missing from context")
		}
		w.WriteHeader(204)
	}))
	check := func(target, header string, want int) {
		t.Helper()
		r := httptest.NewRequest("POST", target, strings.NewReader(`{}`))
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: got %d want %d", target, w.Code, want)
		}
	}
	check("http://127.0.0.1:5235/mcp", "Bearer "+token.Access, 204)
	check("http://127.0.0.1:5235/mcp?access_token="+token.Access, "", 401)
	if _, err := a.storage.db.Exec("UPDATE auth_tokens SET audience='https://other.invalid/mcp'"); err != nil {
		t.Fatal(err)
	}
	check("http://127.0.0.1:5235/mcp", "Bearer "+token.Access, 401)
	token.AccessCreateAt = time.Now().Add(-2 * time.Hour)
	raw, _ := json.Marshal(token)
	if _, err := a.storage.db.Exec("UPDATE auth_tokens SET audience=?,data=?,expires=?", a.Resource(), raw, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	check("http://127.0.0.1:5235/mcp", "Bearer "+token.Access, 401)
}
func TestRateLimiterStaysBounded(t *testing.T) {
	a := testAuth(t)
	for range 8 {
		if !a.allowAttempt("login:one", 8) {
			t.Fatal("limit too early")
		}
	}
	for range 100 {
		if a.allowAttempt("login:one", 8) {
			t.Fatal("limit bypass")
		}
	}
	if len(a.attempts["login:one"]) != 8 {
		t.Fatal("attempt slice grows without bound")
	}
	for i := range 1100 {
		a.allowAttempt(string(rune(i+100)), 1)
	}
	if len(a.attempts) > 1024 {
		t.Fatal("IP map grows without bound")
	}
}

func TestRegistrationReclaimsAbandonedCapacity(t *testing.T) {
	a := testAuth(t)
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour)
	tx, err := a.storage.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 512 {
		id := fmt.Sprintf("pending-%d", i)
		created := old.Unix()
		if i == 0 {
			id = "active"
		}
		if i == 1 {
			id = "recent"
			created = time.Now().Unix()
		}
		client := models.Client{ID: id, Public: true, Domain: `["http://127.0.0.1/callback"]`, UserID: "owner"}
		raw, _ := json.Marshal(client)
		if _, err = tx.ExecContext(ctx, "INSERT INTO auth_clients(id,data,created) VALUES(?,?,?)", id, raw, created); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	active := models.NewToken()
	active.ClientID = "active"
	active.UserID = "owner"
	active.Access = "synthetic-live-access"
	active.AccessCreateAt = time.Now()
	active.AccessExpiresIn = time.Hour
	active.Refresh = "synthetic-live-refresh"
	active.RefreshCreateAt = time.Now()
	active.RefreshExpiresIn = 30 * 24 * time.Hour
	if err = a.storage.Create(ctx, active); err != nil {
		t.Fatal(err)
	}
	expired := models.NewToken()
	expired.ClientID = "pending-2"
	expired.Code = "synthetic-expired-code"
	expired.CodeCreateAt = old
	expired.CodeExpiresIn = time.Minute
	if err = a.storage.Create(ctx, expired); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.Register(mux)
	request := httptest.NewRequest("POST", "http://127.0.0.1:5235/oauth/register", strings.NewReader(`{"client_name":"new client","redirect_uris":["http://127.0.0.1/callback"],"token_endpoint_auth_method":"none"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("capacity was not reclaimed: %d %s", response.Code, response.Body.String())
	}
	var count int
	if err = a.storage.db.QueryRow("SELECT count(*) FROM auth_clients").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("want active, recent, and newly registered client; got %d", count)
	}
	if live, err := a.storage.GetByID(ctx, "active"); err != nil || live == nil {
		t.Fatal("active client was deleted")
	}
	if recent, err := a.storage.GetByID(ctx, "recent"); err != nil || recent == nil {
		t.Fatal("recent pending client was deleted")
	}
	if live, err := a.storage.GetByAccess(ctx, active.Access); err != nil || live == nil {
		t.Fatal("active token was deleted")
	}
	if err = a.storage.db.QueryRow("SELECT count(*) FROM auth_tokens").Scan(&count); err != nil || count != 1 {
		t.Fatalf("expired token cleanup failed: count=%d err=%v", count, err)
	}
}
