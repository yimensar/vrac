package engine

import (
	"context"
	"database/sql"
)

// migrateTypeTokens is migration 2: types become visible tokens of the text,
// which is from now on their only source.
func migrateTypeTokens(e *Engine, tx *sql.Tx) error {
	s := &txState{e: e, ctx: context.Background(), tx: tx}
	rows, err := tx.Query(`SELECT DISTINCT node_seq FROM node_types`)
	if err != nil {
		return err
	}
	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			rows.Close()
			return err
		}
		seqs = append(seqs, seq)
	}
	rows.Close()
	for _, seq := range seqs {
		if err := s.syncTypeTokens(seq); err != nil {
			return err
		}
		if err := s.index(seq); err != nil {
			return err
		}
	}
	return nil
}
