package engine

import (
	"database/sql"
	"sort"
)

// activityContext captures stable identities known at the time of work. It is
// canonical event data: later moves and reference edits must not rewrite it.
func activityContext(tx *sql.Tx, id string) ([]string, error) {
	rows, err := tx.Query(`WITH RECURSIVE ancestors(id,parent_id,system) AS (
  SELECT id,parent_id,system FROM nodes WHERE id=?
  UNION ALL SELECT n.id,n.parent_id,n.system FROM nodes n JOIN ancestors a ON n.id=a.parent_id
 ) SELECT a.id,COALESCE(a.system,''),r.target_id FROM ancestors a LEFT JOIN node_references r ON r.source_id=a.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var ancestor, system string
		var target sql.NullString
		if err = rows.Scan(&ancestor, &system, &target); err != nil {
			return nil, err
		}
		if system == "" {
			set[ancestor] = true
		}
		if target.Valid {
			set[target.String] = true
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}
func referenceIDs(n Node) []string {
	ids := make([]string, 0, len(n.References))
	for _, ref := range n.References {
		ids = append(ids, ref.TargetID)
	}
	return ids
}
