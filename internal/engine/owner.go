package engine

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Owner is the single account of an instance. The server owns password
// hashing and WebAuthn; the engine only stores their state.
type Owner struct {
	Username     string
	PasswordHash string
	// UserHandle is the opaque WebAuthn user ID.
	UserHandle []byte
}

// Passkey is a registered WebAuthn credential, stored as the library's JSON.
type Passkey struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"-"`
	CreatedAt  string          `json:"created_at"`
	LastUsedAt string          `json:"last_used_at,omitempty"`
}

// HasOwner reports whether the owner account exists.
func (e *Engine) HasOwner(ctx context.Context) (bool, error) {
	var n int
	err := e.db.QueryRowContext(ctx, `SELECT count(*) FROM owner`).Scan(&n)
	return n > 0, err
}

// Owner returns the owner account.
func (e *Engine) Owner(ctx context.Context) (Owner, error) {
	var o Owner
	err := e.db.QueryRowContext(ctx, `SELECT username, password_hash, user_handle FROM owner`).Scan(&o.Username, &o.PasswordHash, &o.UserHandle)
	if errors.Is(err, sql.ErrNoRows) {
		return o, fmt.Errorf("%w: no owner account", ErrNotFound)
	}
	return o, err
}

// NewUserHandle returns a random WebAuthn user handle.
func NewUserHandle() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// CreateOwner creates the owner account and, when given, its first passkey,
// in one transaction. It fails if an owner exists.
func (e *Engine) CreateOwner(ctx context.Context, o Owner, first *Passkey) error {
	if o.Username == "" || o.PasswordHash == "" || len(o.UserHandle) == 0 {
		return invalid("username, password and user handle are required")
	}
	return e.write(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM owner`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: the owner account already exists", ErrConflict)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO owner(singleton, password_hash, created_at, username, user_handle) VALUES (1, ?, ?, ?, ?)`,
			o.PasswordHash, e.stamp(), o.Username, o.UserHandle); err != nil {
			return err
		}
		if first != nil {
			return e.insertPasskey(ctx, tx, *first)
		}
		return nil
	})
}

// SetOwnerPassword replaces the owner's password hash, and username when
// not empty, and ends every browser session.
func (e *Engine) SetOwnerPassword(ctx context.Context, username, passwordHash string) error {
	return e.write(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE owner SET password_hash = ?, username = coalesce(nullif(?, ''), username)`, passwordHash, username)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return fmt.Errorf("%w: no owner account", ErrNotFound)
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM sessions`)
		return err
	})
}

// Passkeys lists the owner's passkeys, oldest first.
func (e *Engine) Passkeys(ctx context.Context) ([]Passkey, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT id, name, credential, created_at, coalesce(last_used_at, '') FROM passkeys ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var p Passkey
		var c string
		if err := rows.Scan(&p.ID, &p.Name, &c, &p.CreatedAt, &p.LastUsedAt); err != nil {
			return nil, err
		}
		p.Credential = json.RawMessage(c)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (e *Engine) insertPasskey(ctx context.Context, tx *sql.Tx, p Passkey) error {
	if p.ID == "" || len(p.Credential) == 0 {
		return invalid("a passkey needs an ID and a credential")
	}
	if p.Name == "" {
		p.Name = "Passkey"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO passkeys(id, name, credential, created_at) VALUES (?, ?, ?, ?)`, p.ID, p.Name, string(p.Credential), e.stamp())
	return err
}

// AddPasskey registers a passkey for the owner.
func (e *Engine) AddPasskey(ctx context.Context, p Passkey) error {
	return e.write(ctx, func(tx *sql.Tx) error { return e.insertPasskey(ctx, tx, p) })
}

// UsePasskey stores a passkey's updated credential (its sign count) after a
// sign-in.
func (e *Engine) UsePasskey(ctx context.Context, id string, credential json.RawMessage) error {
	return e.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE passkeys SET credential = ?, last_used_at = ? WHERE id = ?`, string(credential), e.now().UTC().Format(time.RFC3339Nano), id)
		return err
	})
}

// DeletePasskey removes a passkey, never the last one: a password alone
// does not open the instance.
func (e *Engine) DeletePasskey(ctx context.Context, id string) error {
	return e.write(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM passkeys`).Scan(&n); err != nil {
			return err
		}
		if n <= 1 {
			return fmt.Errorf("%w: keep at least one passkey", ErrConflict)
		}
		r, err := tx.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if k, _ := r.RowsAffected(); k == 0 {
			return fmt.Errorf("%w: passkey %s", ErrNotFound, id)
		}
		return nil
	})
}

// ResetPasskeys removes every passkey, for recovery from the command line:
// the next sign-in with the password enrolls a new one.
func (e *Engine) ResetPasskeys(ctx context.Context) (int, error) {
	var n int64
	err := e.write(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `DELETE FROM passkeys`)
		if err == nil {
			n, _ = r.RowsAffected()
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM sessions`)
		}
		return err
	})
	return int(n), err
}
