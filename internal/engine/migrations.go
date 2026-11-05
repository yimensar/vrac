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

// migrateOwner is migration 6: a username and a WebAuthn handle for the
// owner, and the passkeys table.
func migrateOwner(e *Engine, tx *sql.Tx) error {
	columns := map[string]bool{}
	rows, err := tx.Query(`SELECT name FROM pragma_table_info('owner')`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	rows.Close()
	for name, ddl := range map[string]string{
		"username":    `ALTER TABLE owner ADD COLUMN username TEXT NOT NULL DEFAULT 'owner'`,
		"user_handle": `ALTER TABLE owner ADD COLUMN user_handle BLOB`,
	} {
		if !columns[name] {
			if _, err := tx.Exec(ddl); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(schemaV6)
	return err
}
