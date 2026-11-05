package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Authentication storage. The server owns the protocols and hashing; the
// engine only persists their state.

// CreateSession stores a browser session.
func (e *Engine) CreateSession(ctx context.Context, tokenHash string, expires time.Time) error {
	return e.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, e.stamp()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO sessions(token_hash, created_at, expires_at) VALUES (?, ?, ?)`, tokenHash, e.stamp(), expires.UTC().Format(time.RFC3339Nano))
		return err
	})
}

// SessionValid reports whether a session exists and has not expired.
func (e *Engine) SessionValid(ctx context.Context, tokenHash string) (bool, error) {
	var n int
	err := e.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE token_hash = ? AND expires_at > ?`, tokenHash, e.stamp()).Scan(&n)
	return n > 0, err
}

// DeleteSession ends a session.
func (e *Engine) DeleteSession(ctx context.Context, tokenHash string) error {
	return e.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
		return err
	})
}

// OAuthClient is a registered OAuth client.
type OAuthClient struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
	CreatedAt    string   `json:"created_at"`
}

// RegisterClient stores a dynamically registered client.
func (e *Engine) RegisterClient(ctx context.Context, name string, redirectURIs []string) (OAuthClient, error) {
	c := OAuthClient{ID: newID(), Name: name, RedirectURIs: redirectURIs, CreatedAt: e.stamp()}
	uris, _ := json.Marshal(redirectURIs)
	err := e.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO oauth_clients(id, name, redirect_uris, created_at) VALUES (?, ?, ?, ?)`, c.ID, c.Name, string(uris), c.CreatedAt)
		return err
	})
	return c, err
}

// Client returns a registered client.
func (e *Engine) Client(ctx context.Context, id string) (OAuthClient, error) {
	c := OAuthClient{ID: id}
	var uris string
	err := e.db.QueryRowContext(ctx, `SELECT name, redirect_uris, created_at FROM oauth_clients WHERE id = ?`, id).Scan(&c.Name, &uris, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("%w: client %s", ErrNotFound, id)
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(uris), &c.RedirectURIs)
	return c, err
}

// AuthCode is a pending OAuth authorization code.
type AuthCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	Scope       string
	Resource    string
}

// SaveCode stores an authorization code.
func (e *Engine) SaveCode(ctx context.Context, codeHash string, c AuthCode, expires time.Time) error {
	return e.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_codes WHERE expires_at < ?`, e.stamp()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO oauth_codes(code_hash, client_id, redirect_uri, challenge, scope, resource, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			codeHash, c.ClientID, c.RedirectURI, c.Challenge, c.Scope, c.Resource, expires.UTC().Format(time.RFC3339Nano))
		return err
	})
}

// TakeCode consumes an authorization code: it can be used once.
func (e *Engine) TakeCode(ctx context.Context, codeHash string) (AuthCode, error) {
	var c AuthCode
	err := e.write(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT client_id, redirect_uri, challenge, scope, resource FROM oauth_codes WHERE code_hash = ? AND expires_at > ?`, codeHash, e.stamp()).
			Scan(&c.ClientID, &c.RedirectURI, &c.Challenge, &c.Scope, &c.Resource)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: unknown or expired code", ErrNotFound)
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM oauth_codes WHERE code_hash = ?`, codeHash)
		return err
	})
	return c, err
}

// Token kinds.
const (
	TokenAccess   = "access"
	TokenRefresh  = "refresh"
	TokenPersonal = "personal"
)

// TokenInfo describes a stored token.
type TokenInfo struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	ClientID   string `json:"client_id,omitempty"`
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// SaveToken stores a token by hash. A zero expiry never expires.
func (e *Engine) SaveToken(ctx context.Context, tokenHash string, t TokenInfo, expires time.Time) error {
	var exp any
	if !expires.IsZero() {
		exp = expires.UTC().Format(time.RFC3339Nano)
	}
	var client any
	if t.ClientID != "" {
		client = t.ClientID
	}
	return e.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tokens WHERE expires_at < ?`, e.stamp()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO tokens(token_hash, kind, client_id, name, scope, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			tokenHash, t.Kind, client, t.Name, t.Scope, e.stamp(), exp)
		return err
	})
}

// Token returns a valid token by hash and records its use.
func (e *Engine) Token(ctx context.Context, tokenHash string) (TokenInfo, error) {
	t := TokenInfo{ID: tokenHash[:16]}
	var client, exp, used sql.NullString
	err := e.db.QueryRowContext(ctx, `SELECT kind, client_id, name, scope, created_at, expires_at, last_used_at FROM tokens
		WHERE token_hash = ? AND (expires_at IS NULL OR expires_at > ?)`, tokenHash, e.stamp()).
		Scan(&t.Kind, &client, &t.Name, &t.Scope, &t.CreatedAt, &exp, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return t, fmt.Errorf("%w: unknown or expired token", ErrNotFound)
	}
	if err != nil {
		return t, err
	}
	t.ClientID, t.ExpiresAt, t.LastUsedAt = client.String, exp.String, used.String
	// Record use at most once a minute to avoid a write per request.
	now := e.now().UTC()
	if last, err := time.Parse(time.RFC3339Nano, used.String); err != nil || now.Sub(last) > time.Minute {
		err := e.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE tokens SET last_used_at = ? WHERE token_hash = ?`, now.Format(time.RFC3339Nano), tokenHash)
			return err
		})
		if err != nil {
			return t, err
		}
	}
	return t, nil
}

// DeleteToken revokes a token by hash.
func (e *Engine) DeleteToken(ctx context.Context, tokenHash string) error {
	return e.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM tokens WHERE token_hash = ?`, tokenHash)
		return err
	})
}

// Connection is an authorized client or personal token, for review and
// revocation.
type Connection struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// Connections lists personal tokens and OAuth clients holding tokens.
func (e *Engine) Connections(ctx context.Context) ([]Connection, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT substr(token_hash, 1, 16), 'token', name, created_at, coalesce(last_used_at, '') FROM tokens WHERE kind = 'personal'
		UNION ALL
		SELECT c.id, 'client', c.name, c.created_at, coalesce(max(t.last_used_at), '') FROM oauth_clients c JOIN tokens t ON t.client_id = c.id
			WHERE t.expires_at IS NULL OR t.expires_at > ? GROUP BY c.id
		ORDER BY 4 DESC`, e.stamp())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var c Connection
		if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &c.CreatedAt, &c.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Revoke removes a personal token (by ID) or every token of a client.
func (e *Engine) Revoke(ctx context.Context, kind, id string) error {
	return e.write(ctx, func(tx *sql.Tx) error {
		var r sql.Result
		var err error
		switch kind {
		case "token":
			r, err = tx.ExecContext(ctx, `DELETE FROM tokens WHERE kind = 'personal' AND substr(token_hash, 1, 16) = ?`, id)
		case "client":
			r, err = tx.ExecContext(ctx, `DELETE FROM tokens WHERE client_id = ?`, id)
		default:
			return invalid("kind is token or client")
		}
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return fmt.Errorf("%w: connection %s", ErrNotFound, id)
		}
		return nil
	})
}
