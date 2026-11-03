package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yimensar/vrac/internal/auth"
	notemcp "github.com/yimensar/vrac/internal/mcp"
	"github.com/yimensar/vrac/internal/model"
	"github.com/yimensar/vrac/internal/store"
)

type Config struct {
	PublicURL     string
	OwnerPassword string
	DevOrigin     string
	WebDir        string
	AttachmentDir string
}
type Server struct {
	Store   *store.Store
	Auth    *auth.Auth
	Handler http.Handler
}

func New(s *store.Store, cfg Config) (*Server, error) {
	a, err := auth.New(s.DB(), auth.Config{PublicURL: cfg.PublicURL, OwnerPassword: cfg.OwnerPassword, DevOrigin: cfg.DevOrigin})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	a.Register(mux)
	add := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, a.RequireSession(fn)) }
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]string{"status": "ok"}) })
	add("GET /api/notes", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		p, err := s.List(r.Context(), model.Query{Q: q.Get("q"), Tag: q.Get("tag"), State: q.Get("state"), Pinned: q.Get("pinned") == "true", Limit: limit, Cursor: q.Get("cursor"), Sort: q.Get("sort")})
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, p)
	})
	add("GET /api/notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		n, err := s.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, n)
	})
	add("POST /api/notes", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Content        string `json:"content"`
			Pinned         bool   `json:"pinned"`
			Source         string `json:"source"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if err := decode(w, r, &in); err != nil {
			failJSON(w, 400, err.Error())
			return
		}
		if in.Source == "" {
			in.Source = "web"
		}
		n, err := s.Create(r.Context(), store.CreateInput{Content: in.Content, Pinned: in.Pinned, Source: in.Source, IdempotencyKey: in.IdempotencyKey})
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 201, n)
	})
	add("PATCH /api/notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Content *string `json:"content"`
			Pinned  *bool   `json:"pinned"`
			State   *string `json:"state"`
			Version int64   `json:"version"`
		}
		if err := decode(w, r, &in); err != nil {
			failJSON(w, 400, err.Error())
			return
		}
		n, err := s.Update(r.Context(), r.PathValue("id"), store.UpdateInput{Content: in.Content, Pinned: in.Pinned, State: in.State, Version: in.Version})
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, n)
	})
	add("GET /api/notes/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		n, err := s.History(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		if n == nil {
			n = []model.Note{}
		}
		reply(w, 200, n)
	})
	add("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		tags, err := s.Tags(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		if tags == nil {
			tags = []model.Tag{}
		}
		reply(w, 200, tags)
	})
	add("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		stats, err := s.Stats(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, 200, stats)
	})
	add("GET /api/export", func(w http.ResponseWriter, r *http.Request) {
		data, err := s.Export(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="vrac-export-`+time.Now().UTC().Format("2006-01-02")+`.json"`)
		reply(w, 200, data)
	})
	attachmentHandler := func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		attachment, err := s.Attachment(r.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		if filepath.Base(id) != id || filepath.Base(attachment.Filename) != attachment.Filename || id == "." || attachment.Filename == "." {
			failJSON(w, 400, "invalid attachment path")
			return
		}
		path := filepath.Join(cfg.AttachmentDir, id, attachment.Filename)
		f, err := os.Open(path)
		if err != nil {
			failJSON(w, 404, "attachment content is not available locally")
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			failJSON(w, 404, "attachment unavailable")
			return
		}
		w.Header().Set("Content-Type", attachment.Type)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Filename}))
		w.Header().Set("Content-Security-Policy", "sandbox")
		http.ServeContent(w, r, attachment.Filename, info.ModTime(), f)
	}
	add("GET /api/attachments/{id}", attachmentHandler)
	// Preserve original Memos Markdown without rewriting source bytes at import.
	add("GET /file/attachments/{id}/{filename}", attachmentHandler)
	mux.Handle("/mcp", notemcp.Handler(s, a))
	// Unknown API routes must remain JSON 404s instead of falling through to the SPA.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { failJSON(w, 404, "endpoint not found") })
	mux.Handle("/", spa(cfg.WebDir))
	expected, _ := url.Parse(cfg.PublicURL)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		// A fixed public host plus origin checks protects loopback instances from DNS rebinding.
		if !strings.EqualFold(r.Host, expected.Host) {
			failJSON(w, 421, "request host does not match configured public URL")
			return
		}
		mux.ServeHTTP(w, r)
	})
	return &Server{Store: s, Auth: a, Handler: handler}, nil
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("application/json required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("invalid JSON")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("one JSON object required")
	}
	return nil
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failJSON(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		failJSON(w, 404, "note not found")
	case errors.Is(err, store.ErrConflict):
		failJSON(w, 409, "note changed; reload before editing")
	case errors.Is(err, store.ErrInvalid):
		failJSON(w, 400, err.Error())
	default:
		failJSON(w, 500, "operation could not be completed")
	}
}
func spa(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.NotFound(w, r)
			return
		}
		if dir == "" {
			http.Error(w, "Build the frontend with pnpm --dir web build, then start Vrac with -web web/dist.", 503)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		path := filepath.Join(dir, clean)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			http.ServeFile(w, r, path)
			return
		}
		if filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}
