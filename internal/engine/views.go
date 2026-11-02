package engine

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// NodeView combines a current node and its context from one database snapshot.
// Children is omitted when the caller only requests node and ancestor content.
type NodeView struct {
	Node     Node      `json:"node"`
	Path     []Node    `json:"path"`
	Children *NodePage `json:"children,omitempty"`
}

// Read returns views in input order, including repeated IDs. Every requested
// node, path and optional child page belongs to the same read transaction.
func (e *Engine) Read(ids []string, includeChildren bool, limit int) ([]NodeView, error) {
	if len(ids) < 1 || len(ids) > 10 {
		return nil, fmt.Errorf("%w: read requires between 1 and 10 node IDs", ErrInvalid)
	}
	limit, err := pageLimit(limit)
	if err != nil {
		return nil, err
	}
	var result []NodeView
	err = e.read(func(tx *sql.Tx) error {
		nodes := make([]Node, len(ids))
		for i, id := range ids {
			if id == "" {
				return fmt.Errorf("%w: read requires nonempty node IDs", ErrInvalid)
			}
			var err error
			nodes[i], err = getNode(tx, id, false)
			if err != nil {
				return err
			}
		}
		paths, err := pathsFor(tx, ids)
		if err != nil {
			return err
		}
		result = make([]NodeView, 0, len(ids))
		for i, id := range ids {
			p, ok := paths[id]
			if !ok || len(p) == 0 {
				return ErrNotFound
			}
			nodes[i].JournalDate = p[len(p)-1].JournalDate
			view := NodeView{Node: nodes[i], Path: p}
			if includeChildren {
				page, err := e.children(tx, id, "", limit)
				if err != nil {
					return err
				}
				view.Children = &page
			}
			result = append(result, view)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReadRevision returns one exact historical snapshot, including archived notes.
// Its reference labels and bindings remain those recorded in that revision.
func (e *Engine) ReadRevision(id string, revision int64) (Revision, error) {
	if revision < 1 {
		return Revision{}, fmt.Errorf("%w: revision must be positive", ErrInvalid)
	}
	var result Revision
	err := e.read(func(tx *sql.Tx) error {
		if _, err := getNode(tx, id, true); err != nil {
			return err
		}
		var snapshot string
		err := tx.QueryRow("SELECT revision,at,summary,snapshot FROM revisions WHERE node_id=? AND revision=?", id, revision).Scan(&result.Revision, &result.At, &result.Summary, &snapshot)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: revision", ErrNotFound)
		}
		if err != nil {
			return err
		}
		return json.Unmarshal([]byte(snapshot), &result.Node)
	})
	if err != nil {
		return Revision{}, err
	}
	return result, nil
}

// TagPage is an ordered cursor page of distinct tags on current nodes.
type TagPage struct {
	Tags       []string `json:"tags"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// TagsPage seeks by canonical tag, without rescanning earlier pages. Cursors
// are bound to the normalized prefix; changing page size preserves their scope.
func (e *Engine) TagsPage(prefix, cursor string, limit int) (TagPage, error) {
	limit, err := pageLimit(limit)
	if err != nil {
		return TagPage{}, err
	}
	prefix = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(prefix), "#"))
	if prefix != "" {
		if _, err := CanonicalTag(prefix); err != nil {
			return TagPage{}, err
		}
	}
	scope := "tags:" + prefix
	continuation, err := decodeCursor(cursor, scope)
	if err != nil {
		return TagPage{}, err
	}
	if cursor != "" {
		canonical, err := CanonicalTag(continuation.ID)
		if err != nil || canonical != continuation.ID || !strings.HasPrefix(continuation.ID, prefix) {
			return TagPage{}, fmt.Errorf("%w: invalid tag cursor", ErrInvalid)
		}
	}
	result := TagPage{Tags: []string{}}
	err = e.read(func(tx *sql.Tx) error {
		query := "SELECT DISTINCT t.tag FROM node_tags t WHERE t.tag>=? AND t.tag<?"
		args := []any{prefix, prefix + "\U0010ffff"}
		if cursor != "" {
			query += " AND t.tag>?"
			args = append(args, continuation.ID)
		}
		query += " AND EXISTS(SELECT 1 FROM nodes n WHERE n.id=t.node_id AND n.deleted=0) ORDER BY t.tag LIMIT ?"
		args = append(args, limit+1)
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var tag string
			if err := rows.Scan(&tag); err != nil {
				return err
			}
			result.Tags = append(result.Tags, tag)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(result.Tags) > limit {
			result.NextCursor = encodeCursor(pageCursor{Scope: scope, ID: result.Tags[limit-1]})
			result.Tags = result.Tags[:limit]
		}
		return nil
	})
	if err != nil {
		return TagPage{}, err
	}
	return result, nil
}
