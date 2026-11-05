package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	positionGap   = 1 << 16
	maxDraftNodes = 1000
	maxDepth      = 50
	maxAliasLen   = 200
	maxNameKeyLen = 200
)

// txState carries one mutation transaction.
type txState struct {
	e          *Engine
	ctx        context.Context
	tx         *sql.Tx
	actor      Actor
	now        string
	typeIDs    map[string]int64
	allowTypes map[string]bool
	creations  map[string]Creation
	created    map[string]int64
	briefs     []Brief
	unresolved []Unresolved
	reported   map[string]bool
}

func (e *Engine) mutate(ctx context.Context, actor Actor, requestID string, payload any, fn func(*txState) ([]int64, error)) (Result, error) {
	if strings.TrimSpace(actor.Name) == "" || strings.TrimSpace(actor.Source) == "" {
		return Result{}, invalid("actor name and source are required")
	}
	if requestID == "" || len(requestID) > 200 {
		return Result{}, invalid("request_id is required (at most 200 characters)")
	}
	hash := hashJSON(payload)
	var res Result
	err := e.write(ctx, func(tx *sql.Tx) error {
		var storedHash, stored string
		err := tx.QueryRowContext(ctx, `SELECT payload_hash, response FROM requests WHERE actor = ? AND request_id = ?`, actor.Name, requestID).Scan(&storedHash, &stored)
		if err == nil {
			if storedHash != hash {
				return fmt.Errorf("%w: request_id %q was already used with a different payload", ErrConflict, requestID)
			}
			return json.Unmarshal([]byte(stored), &res)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		s := &txState{e: e, ctx: ctx, tx: tx, actor: actor, now: e.stamp(), typeIDs: map[string]int64{},
			allowTypes: map[string]bool{}, creations: map[string]Creation{}, created: map[string]int64{}, reported: map[string]bool{}}
		seqs, err := fn(s)
		if err != nil {
			return err
		}
		nodes := make([]Node, 0, len(seqs))
		for _, seq := range seqs {
			n, err := e.nodeBySeq(ctx, tx, seq)
			if err != nil {
				return err
			}
			nodes = append(nodes, n)
		}
		refs, err := e.hydrate(ctx, tx, nodes)
		if err != nil {
			return err
		}
		// Results also name their parents, so callers know where nodes live.
		var parents []string
		for _, n := range nodes {
			if n.ParentID != "" {
				parents = append(parents, n.ParentID)
			}
		}
		more, err := e.refMap(ctx, tx, parents)
		if err != nil {
			return err
		}
		for id, r := range more {
			if _, ok := refs[id]; !ok {
				refs[id] = r
			}
		}
		if s.briefs == nil {
			s.briefs = []Brief{}
		}
		res = Result{Nodes: nodes, Refs: refs, Created: s.briefs}
		body, err := json.Marshal(res)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO requests(actor, request_id, payload_hash, response, at) VALUES (?, ?, ?, ?, ?)`, actor.Name, requestID, hash, string(body), s.now)
		return err
	})
	return res, err
}

func (s *txState) allow(creations []Creation, types []string) error {
	for _, t := range types {
		t = strings.ToLower(strings.TrimSpace(t))
		if err := validTypeName(t); err != nil {
			return err
		}
		s.allowTypes[t] = true
	}
	for _, c := range creations {
		name := strings.TrimSpace(c.Name)
		if name == "" || len(name) > maxNameKeyLen || strings.Contains(name, "[[") {
			return invalid("creation names have 1 to %d characters and no [[", maxNameKeyLen)
		}
		c.Name = name
		s.creations[nameKey(name)] = c
	}
	return nil
}

func (s *txState) fail() error {
	if len(s.unresolved) == 0 {
		return nil
	}
	return &UnresolvedError{Items: s.unresolved}
}

func (s *txState) report(kind, name string, candidates []Brief) {
	key := kind + "\x00" + nameKey(name)
	if s.reported[key] {
		return
	}
	s.reported[key] = true
	if candidates == nil {
		candidates = []Brief{}
	}
	s.unresolved = append(s.unresolved, Unresolved{Kind: kind, Name: name, Candidates: candidates})
}
