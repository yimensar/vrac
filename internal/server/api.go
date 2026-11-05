package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/yan-imensar/vrac/internal/engine"
)

func intParam(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		failEngine(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// TodayView is the home screen: today's journal, agenda and due tasks.
type TodayView struct {
	Today  string         `json:"today"`
	Day    engine.DayView `json:"day"`
	Agenda engine.Page    `json:"agenda"`
	Due    engine.Page    `json:"due"`
}

func (s *Server) todayView(r *http.Request) (TodayView, error) {
	ctx := r.Context()
	today := s.engine.Today()
	v := TodayView{Today: today}
	var err error
	if v.Day, err = s.engine.Day(ctx, today, "", 0); err != nil {
		return v, err
	}
	if v.Agenda, err = s.engine.Search(ctx, engine.Query{Types: []string{engine.TypeMeeting, engine.TypeEvent}, From: today, To: today, Scheduled: true, Sort: "date_asc", Limit: 50}); err != nil {
		return v, err
	}
	v.Due, err = s.engine.Search(ctx, engine.Query{Types: []string{engine.TypeTask}, Status: engine.StatusOpen, To: today, Scheduled: true, Sort: "date_asc", Limit: 100})
	return v, err
}

func (s *Server) today(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.todayView(r)
	respond(w, v, err)
}

func (s *Server) days(w http.ResponseWriter, r *http.Request, p principal) {
	days, err := s.engine.Days(r.Context(), r.URL.Query().Get("before"), intParam(r, "limit"))
	respond(w, map[string]any{"days": days}, err)
}

func (s *Server) day(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.Day(r.Context(), r.PathValue("day"), r.URL.Query().Get("cursor"), intParam(r, "limit"))
	respond(w, v, err)
}

func (s *Server) roots(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.Roots(r.Context(), r.URL.Query().Get("cursor"), intParam(r, "limit"))
	respond(w, v, err)
}

func (s *Server) node(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.GetNode(r.Context(), r.PathValue("id"))
	respond(w, v, err)
}

func (s *Server) children(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.Children(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"), intParam(r, "limit"))
	respond(w, v, err)
}

func (s *Server) sections(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.NodeSections(r.Context(), r.PathValue("id"))
	respond(w, v, err)
}

// Mutation bodies carry an optional source next to the engine request.

func (s *Server) create(w http.ResponseWriter, r *http.Request, p principal) {
	var b struct {
		engine.CreateRequest
		Source string `json:"source,omitempty"`
	}
	if !decode(w, r, &b) {
		return
	}
	v, err := s.engine.Create(r.Context(), p.actor(b.Source), b.CreateRequest)
	respond(w, v, err)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request, p principal) {
	var b struct {
		engine.UpdateRequest
		Source string `json:"source,omitempty"`
	}
	if !decode(w, r, &b) {
		return
	}
	b.ID = r.PathValue("id")
	v, err := s.engine.Update(r.Context(), p.actor(b.Source), b.UpdateRequest)
	respond(w, v, err)
}

func (s *Server) move(w http.ResponseWriter, r *http.Request, p principal) {
	var b struct {
		engine.MoveRequest
		Source string `json:"source,omitempty"`
	}
	if !decode(w, r, &b) {
		return
	}
	b.ID = r.PathValue("id")
	v, err := s.engine.Move(r.Context(), p.actor(b.Source), b.MoveRequest)
	respond(w, v, err)
}

type revisionBody struct {
	RequestID        string `json:"request_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Source           string `json:"source,omitempty"`
}

func (s *Server) trash(w http.ResponseWriter, r *http.Request, p principal) {
	var b revisionBody
	if !decode(w, r, &b) {
		return
	}
	v, err := s.engine.Trash(r.Context(), p.actor(b.Source), engine.TrashRequest{RequestID: b.RequestID, ID: r.PathValue("id"), ExpectedRevision: b.ExpectedRevision})
	respond(w, v, err)
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request, p principal) {
	var b revisionBody
	if !decode(w, r, &b) {
		return
	}
	v, err := s.engine.Restore(r.Context(), p.actor(b.Source), engine.TrashRequest{RequestID: b.RequestID, ID: r.PathValue("id"), ExpectedRevision: b.ExpectedRevision})
	respond(w, v, err)
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request, p principal) {
	var b revisionBody
	if !decode(w, r, &b) {
		return
	}
	v, err := s.engine.Undo(r.Context(), p.actor(b.Source), engine.UndoRequest{RequestID: b.RequestID, ID: r.PathValue("id"), ExpectedRevision: b.ExpectedRevision})
	respond(w, v, err)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request, p principal) {
	var q engine.Query
	if !decode(w, r, &q) {
		return
	}
	v, err := s.engine.Search(r.Context(), q)
	respond(w, v, err)
}

func (s *Server) resolve(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.Resolve(r.Context(), r.URL.Query().Get("name"), intParam(r, "limit"))
	respond(w, map[string]any{"candidates": v}, err)
}

func (s *Server) types(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.Types(r.Context())
	respond(w, map[string]any{"types": v}, err)
}

func (s *Server) createType(w http.ResponseWriter, r *http.Request, p principal) {
	var b struct {
		RequestID string `json:"request_id"`
		Name      string `json:"name"`
		Source    string `json:"source,omitempty"`
	}
	if !decode(w, r, &b) {
		return
	}
	v, err := s.engine.CreateType(r.Context(), p.actor(b.Source), b.RequestID, b.Name)
	respond(w, v, err)
}

func (s *Server) typeView(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.TypeTable(r.Context(), r.PathValue("name"))
	respond(w, v, err)
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.Activity(r.Context(), r.URL.Query().Get("actor"), r.URL.Query().Get("cursor"), intParam(r, "limit"))
	respond(w, v, err)
}

func (s *Server) export(w http.ResponseWriter, r *http.Request, p principal) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vrac-%s.json"`, time.Now().Format("2006-01-02")))
	if err := s.engine.Export(r.Context(), w); err != nil {
		s.log.Error("export failed", "error", err)
	}
}

func (s *Server) preferences(w http.ResponseWriter, r *http.Request, p principal) {
	v, err := s.engine.Preferences(r.Context())
	respond(w, v, err)
}

func (s *Server) setPreferences(w http.ResponseWriter, r *http.Request, p principal) {
	var b engine.Preferences
	if !decode(w, r, &b) {
		return
	}
	v, err := s.engine.SetPreferences(r.Context(), b)
	respond(w, v, err)
}
