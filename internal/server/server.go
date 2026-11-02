package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yan-imensar/vrac/internal/engine"
)

const maxBody = 2 << 20

type Options struct {
	Token, OAuthIssuer, OAuthAudience, OAuthScope string
	WebOIDCClientID, WebOIDCProviderName          string
	Dev, NoWeb                                    bool
	Timezone                                      string
	Version                                       string
	Assets                                        fs.FS
	TempDir                                       string
}

type Server struct {
	engine   *engine.Engine
	options  Options
	auth     *auth
	location *time.Location
	handler  http.Handler
}

func New(ctx context.Context, e *engine.Engine, opts Options) (*Server, error) {
	if opts.Timezone == "" {
		opts.Timezone = "Europe/Paris"
	}
	loc, err := time.LoadLocation(opts.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}
	if opts.OAuthScope == "" {
		opts.OAuthScope = "vrac"
	}
	if opts.NoWeb && opts.WebOIDCClientID != "" {
		return nil, errors.New("web OIDC requires the web interface")
	}
	a, err := newAuth(ctx, authOptions{Token: opts.Token, Dev: opts.Dev, OAuthIssuer: opts.OAuthIssuer, OAuthAudience: opts.OAuthAudience, OAuthScope: opts.OAuthScope, WebOIDCClientID: opts.WebOIDCClientID, WebOIDCProviderName: opts.WebOIDCProviderName})
	if err != nil {
		return nil, err
	}
	s := &Server{engine: e, options: opts, auth: a, location: loc}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": opts.Version})
	})
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", a.metadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", a.metadata)
	mux.HandleFunc("POST /api/session", a.login)
	mux.HandleFunc("DELETE /api/session", a.logout)
	mux.HandleFunc("GET /auth/config", a.browserConfig)
	mux.HandleFunc("GET /auth/login", a.browserLogin)
	mux.HandleFunc("GET /auth/callback", a.browserCallback)
	mux.Handle("/mcp", s.protect(s.mcpHandler(), false))
	api := http.NewServeMux()
	api.HandleFunc("GET /api/bootstrap", s.bootstrap)
	api.HandleFunc("GET /api/nodes/{id}", s.node)
	api.HandleFunc("GET /api/nodes/{id}/children", s.children)
	api.HandleFunc("POST /api/nodes", s.create)
	api.HandleFunc("POST /api/capture", s.capture)
	api.HandleFunc("PATCH /api/nodes/{id}", s.update)
	api.HandleFunc("POST /api/nodes/{id}/move", s.move)
	api.HandleFunc("POST /api/nodes/{id}/restore", s.restore)
	api.HandleFunc("DELETE /api/nodes/{id}", s.delete)
	api.HandleFunc("GET /api/search", s.search)
	api.HandleFunc("GET /api/tags", s.tags)
	api.HandleFunc("GET /api/nodes/{id}/revisions", s.revisions)
	api.HandleFunc("GET /api/nodes/{id}/backlinks", s.backlinks)
	api.HandleFunc("GET /api/export", s.export)
	mux.Handle("/api/", s.protect(api, true))
	if !opts.NoWeb {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				writeError(w, http.StatusMethodNotAllowed, "method", "Method not allowed")
				return
			}
			s.ui(w, r)
		})
	}
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if opts.Dev && !loopbackHost(r.Host) {
			writeError(w, http.StatusForbidden, "host", "Development requests require a loopback Host")
			return
		}
		defer func() {
			if err := recover(); err != nil {
				slog.Error("request failed", "method", r.Method, "path", r.URL.Path)
				writeError(w, 500, "internal", "Request failed")
			}
		}()
		mux.ServeHTTP(w, r)
	})
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }
func (s *Server) today() string                                    { return time.Now().In(s.location).Format("2006-01-02") }

func (s *Server) protect(next http.Handler, cookies bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r, s.options.Dev) {
			writeError(w, 403, "origin", "Cross-origin requests are not allowed")
			return
		}
		if !s.auth.authorized(r, cookies) {
			s.auth.challenge(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if content := r.Header.Get("Content-Type"); content != "" {
		mediaType, _, err := mime.ParseMediaType(content)
		if err != nil || mediaType != "application/json" {
			return errors.New("Content-Type must be application/json")
		}
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errors.New("body must contain exactly one JSON value")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func engineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrNotFound):
		writeError(w, 404, "not_found", err.Error())
	case errors.Is(err, engine.ErrConflict):
		writeError(w, 409, "conflict", err.Error())
	case errors.Is(err, engine.ErrInvalid), errors.Is(err, engine.ErrProtected):
		writeError(w, 400, "invalid_request", err.Error())
	default:
		slog.Error("engine operation failed", "error", err)
		writeError(w, 500, "storage", "Storage operation failed; retry after checking the server")
	}
}
func pageLimit(r *http.Request) (int, error) {
	if r.URL.Query().Get("limit") == "" {
		return 50, nil
	}
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n < 1 || n > 100 {
		return 0, fmt.Errorf("limit must be between 1 and 100")
	}
	return n, nil
}
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = s.today()
	}
	day, err := s.engine.EnsureDay(date)
	if err != nil {
		engineError(w, err)
		return
	}
	j, err := s.engine.Journal()
	if err != nil {
		engineError(w, err)
		return
	}
	tagPage, err := s.engine.TagsPage("", "", 100)
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"today": day, "journal": j, "date": date, "timezone": s.options.Timezone, "tags": tagPage.Tags, "tags_next_cursor": tagPage.NextCursor, "version": s.options.Version})
}
func (s *Server) node(w http.ResponseWriter, r *http.Request) {
	views, err := s.engine.Read([]string{r.PathValue("id")}, true, 50)
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, views[0])
}
func (s *Server) children(w http.ResponseWriter, r *http.Request) {
	limit, err := pageLimit(r)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	page, err := s.engine.Children(r.PathValue("id"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, page)
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var in engine.CreateInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if in.RequestID == "" {
		in.RequestID = r.Header.Get("Idempotency-Key")
	}
	n, err := s.engine.Create(in)
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"node": n})
}

type captureInput struct {
	Date      string               `json:"date,omitempty"`
	Nodes     []engine.CreateInput `json:"nodes"`
	RequestID string               `json:"request_id,omitempty"`
}

func (s *Server) capture(w http.ResponseWriter, r *http.Request) {
	var in captureInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if in.RequestID == "" {
		in.RequestID = r.Header.Get("Idempotency-Key")
	}
	nodes, err := s.engine.Capture(in.Date, in.Nodes, in.RequestID, s.today())
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"nodes": nodes})
}
func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	var in engine.UpdateInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if in.RequestID == "" {
		in.RequestID = r.Header.Get("Idempotency-Key")
	}
	n, err := s.engine.Update(r.PathValue("id"), in, s.today())
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"node": n})
}
func (s *Server) move(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentID         string          `json:"parent_id"`
		Placement        json.RawMessage `json:"placement"`
		ExpectedRevision int64           `json:"expected_revision"`
		RequestID        string          `json:"request_id,omitempty"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	placement, err := parsePlacement(body.Placement)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	n, err := s.engine.Move(r.PathValue("id"), engine.MoveInput{ParentID: body.ParentID, Placement: placement, ExpectedRevision: body.ExpectedRevision, RequestID: body.RequestID}, s.today())
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"node": n})
}
func parsePlacement(raw json.RawMessage) (engine.Placement, error) {
	var p engine.Placement
	if len(raw) == 0 || string(raw) == "null" {
		return p, nil
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		switch str {
		case "first":
			p.First = true
		case "last":
		default:
			return p, errors.New("placement must be first, last, before, or after")
		}
		return p, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return p, errors.New("invalid placement")
	}
	return p, nil
}
func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExpectedRevision int64  `json:"expected_revision"`
		RequestID        string `json:"request_id,omitempty"`
	}
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if err := s.engine.Delete(r.PathValue("id"), in.ExpectedRevision, "", in.RequestID, s.today()); err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision         int64  `json:"revision"`
		ExpectedRevision int64  `json:"expected_revision"`
		RequestID        string `json:"request_id,omitempty"`
	}
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	n, err := s.engine.Restore(r.PathValue("id"), in.Revision, in.ExpectedRevision, "", in.RequestID, s.today())
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"node": n})
}
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	limit, err := pageLimit(r)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	q := r.URL.Query()
	page, err := s.engine.Search(engine.SearchFilters{Query: q.Get("q"), Tag: q.Get("tag"), Tags: q["tags"], SubjectID: q.Get("subject"), From: q.Get("from"), To: q.Get("to"), ActivityFrom: q.Get("activity_from"), ActivityTo: q.Get("activity_to"), Cursor: q.Get("cursor"), Limit: limit})
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, page)
}
func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	limit, err := pageLimit(r)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	page, err := s.engine.TagsPage(r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, page)
}
func (s *Server) backlinks(w http.ResponseWriter, r *http.Request) {
	limit, err := pageLimit(r)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	page, err := s.engine.Backlinks(r.PathValue("id"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		engineError(w, err)
		return
	}
	writeJSON(w, 200, page)
}
func (s *Server) revisions(w http.ResponseWriter, r *http.Request) {
	limit, err := pageLimit(r)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	page, err := s.engine.Revisions(r.PathValue("id"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		engineError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(page.Revisions))
	for _, v := range page.Revisions {
		items = append(items, map[string]any{"revision": v.Revision, "created_at": v.At, "at": v.At, "text": v.Node.Text, "tags": v.Node.Tags, "summary": v.Summary, "node": v.Node})
	}
	writeJSON(w, 200, map[string]any{"revisions": items, "next_cursor": page.NextCursor})
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	// Prepare a complete artifact before acknowledging the download. Storage
	// failures must not become an apparently successful, truncated JSON export.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(15 * time.Minute))
	f, err := os.CreateTemp(s.options.TempDir, "vrac-export-*.json")
	if err != nil {
		engineError(w, err)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := s.engine.Export(f); err != nil {
		engineError(w, err)
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		engineError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="vrac-export.json"`)
	http.ServeContent(w, r, "vrac-export.json", time.Time{}, f)
}

func loopbackHost(hostport string) bool {
	host := hostport
	if value, _, err := net.SplitHostPort(hostport); err == nil {
		host = value
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
func (s *Server) ui(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, 404, "not_found", "Unknown API endpoint")
		return
	}
	if s.options.Assets == nil {
		http.Error(w, "Web UI is not included in this build. The MCP endpoint is /mcp.", 503)
		return
	}
	if r.URL.Path != "/" {
		http.FileServerFS(s.options.Assets).ServeHTTP(w, r)
		return
	}
	data, err := fs.ReadFile(s.options.Assets, "index.html")
	if err != nil {
		http.Error(w, "Build the web UI with: npm --prefix web ci && npm --prefix web run build", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}
