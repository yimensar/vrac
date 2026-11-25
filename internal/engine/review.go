package engine

import (
	"context"
	"strings"
	"time"
)

// A to-do comes back to review once nobody has changed or looked at it for
// reviewEvery, or for reviewSomedayEvery when it carries the someday tag.
const (
	reviewEvery        = 7 * 24 * time.Hour
	reviewSomedayEvery = 30 * 24 * time.Hour
	reviewLimit        = 500
)

// ReviewQueue lists the open to-dos due for review, the longest unseen
// first, with the subjects each concerns. The web review and AI clients
// walk the same queue.
func (e *Engine) ReviewQueue(ctx context.Context) (Page, error) {
	prefs, err := e.Preferences(ctx)
	if err != nil {
		return Page{}, err
	}
	now := e.now().UTC()
	week := now.Add(-reviewEvery).Format(time.RFC3339Nano)
	month := now.Add(-reviewSomedayEvery).Format(time.RFC3339Nano)
	const seen = `max(n.updated_at, coalesce(n.reviewed_at, ''))`
	nodes, err := e.nodesWhere(ctx, e.db, `WHERE n.kind = 'node' AND n.trashed_at IS NULL AND n.status = ?
		AND `+seen+` < CASE WHEN EXISTS (SELECT 1 FROM node_types nt JOIN types t ON t.id = nt.type_id
			WHERE nt.node_seq = n.seq AND t.name = ?) THEN ? ELSE ? END
		ORDER BY `+seen+`, n.seq LIMIT ?`, StatusOpen, prefs.TasksSomeday, month, week, reviewLimit)
	if err != nil {
		return Page{}, err
	}
	page := Page{Nodes: nodes}
	return page, e.finish(ctx, &page, true)
}

// The decisions of a review.
const (
	ReviewKeep    = "keep"
	ReviewDone    = "done"
	ReviewDate    = "date"
	ReviewTrash   = "trash"
	ReviewNext    = "next"
	ReviewSomeday = "someday"
)

// decide applies a review decision other than keep, each through the
// mutation it stands for.
func (e *Engine) decide(ctx context.Context, actor Actor, req ReviewRequest) (Result, error) {
	n, err := e.nodeByID(ctx, e.db, req.ID)
	if err != nil {
		return Result{}, err
	}
	if n.Status == "" || n.Trashed {
		return Result{}, invalid("node %s is not a to-do", req.ID)
	}
	update := UpdateRequest{RequestID: req.RequestID, ID: req.ID, ExpectedRevision: req.ExpectedRevision}
	switch req.Action {
	case ReviewDone:
		update.Status = ptrTo(StatusDone)
	case ReviewDate:
		if strings.TrimSpace(req.DateText) == "" {
			return Result{}, invalid("date needs date_text")
		}
		update.DateText = ptrTo(req.DateText)
	case ReviewTrash:
		return e.Trash(ctx, actor, TrashRequest{RequestID: req.RequestID, ID: req.ID, ExpectedRevision: req.ExpectedRevision})
	case ReviewNext, ReviewSomeday:
		prefs, err := e.Preferences(ctx)
		if err != nil {
			return Result{}, err
		}
		add, other := prefs.TasksNext, prefs.TasksSomeday
		if req.Action == ReviewSomeday {
			add, other = other, add
		}
		if add == "" {
			return Result{}, invalid("no %s tag is set in the preferences", req.Action)
		}
		text, err := Retag(n.Text, add, other)
		if err != nil {
			return Result{}, err
		}
		update.Input = &text
	default:
		return Result{}, invalid("action is keep, done, date, trash, next or someday")
	}
	return e.Update(ctx, actor, update)
}

func ptrTo(s string) *string { return &s }

// Retag returns text with the #tag tokens named in remove taken out and
// #add appended unless already there. An empty add only removes.
func Retag(text, add string, remove ...string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	p, err := parseInput(text)
	if err != nil {
		return "", err
	}
	drop := map[string]bool{}
	for _, r := range remove {
		drop[strings.ToLower(r)] = true
	}
	var b strings.Builder
	last, has := 0, false
	for _, t := range p.Tokens {
		if t.Ref >= 0 {
			continue
		}
		if t.Type == add {
			has = true
		}
		if drop[t.Type] && t.Type != add {
			b.WriteString(text[last:t.Start])
			last = t.End
		}
	}
	b.WriteString(text[last:])
	out := normalizeLines(b.String())
	if add != "" && !has {
		out = strings.TrimSpace(out + " #" + add)
	}
	return out, nil
}
