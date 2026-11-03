package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-oauth2/oauth2/v4"
	"github.com/go-oauth2/oauth2/v4/models"
)

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func digest(v string) string {
	if v == "" {
		return ""
	}
	b := sha256.Sum256([]byte(v))
	return hex.EncodeToString(b[:])
}

type sqlStorage struct {
	db       *sql.DB
	resource string
}

func (s *sqlStorage) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS auth_owner(id INTEGER PRIMARY KEY CHECK(id=1),password_hash BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS auth_sessions(token_hash TEXT PRIMARY KEY,expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS auth_clients(id TEXT PRIMARY KEY,data BLOB NOT NULL,created INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS auth_tokens(id TEXT PRIMARY KEY,code_hash TEXT NOT NULL,access_hash TEXT NOT NULL,refresh_hash TEXT NOT NULL,data BLOB NOT NULL,audience TEXT NOT NULL,expires INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS auth_tokens_code ON auth_tokens(code_hash);
 CREATE INDEX IF NOT EXISTS auth_tokens_access ON auth_tokens(access_hash);
 CREATE INDEX IF NOT EXISTS auth_tokens_refresh ON auth_tokens(refresh_hash);`)
	return err
}
func (s *sqlStorage) GetByID(ctx context.Context, id string) (oauth2.ClientInfo, error) {
	var raw []byte
	if err := s.db.QueryRowContext(ctx, "SELECT data FROM auth_clients WHERE id=?", id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var c models.Client
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
func (s *sqlStorage) saveClient(ctx context.Context, c *models.Client) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO auth_clients(id,data,created) VALUES(?,?,?)", c.ID, raw, time.Now().Unix())
	return err
}

func (s *sqlStorage) pruneRegistrations(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM auth_tokens WHERE expires<=? OR (code_hash='' AND access_hash='' AND refresh_hash='')`, now.Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM auth_clients WHERE created<? AND NOT EXISTS (
	 SELECT 1 FROM auth_tokens t WHERE json_extract(t.data,'$.ClientID')=auth_clients.id AND t.expires>?
	)`, now.Add(-24*time.Hour).Unix(), now.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *sqlStorage) Create(ctx context.Context, info oauth2.TokenInfo) error {
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	exp := info.GetRefreshCreateAt().Add(info.GetRefreshExpiresIn())
	if info.GetRefresh() == "" {
		exp = info.GetAccessCreateAt().Add(info.GetAccessExpiresIn())
	}
	if info.GetCode() != "" {
		exp = info.GetCodeCreateAt().Add(info.GetCodeExpiresIn())
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO auth_tokens(id,code_hash,access_hash,refresh_hash,data,audience,expires) VALUES(?,?,?,?,?,?,?)", randomToken(), digest(info.GetCode()), digest(info.GetAccess()), digest(info.GetRefresh()), raw, s.resource, exp.Unix())
	return err
}
func (s *sqlStorage) remove(ctx context.Context, column, token string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE auth_tokens SET "+column+"='' WHERE "+column+"=?", digest(token))
	return err
}
func (s *sqlStorage) RemoveByCode(ctx context.Context, v string) error {
	return s.remove(ctx, "code_hash", v)
}
func (s *sqlStorage) RemoveByAccess(ctx context.Context, v string) error {
	return s.remove(ctx, "access_hash", v)
}
func (s *sqlStorage) RemoveByRefresh(ctx context.Context, v string) error {
	return s.remove(ctx, "refresh_hash", v)
}
func (s *sqlStorage) token(ctx context.Context, column, v string) (oauth2.TokenInfo, error) {
	if v == "" {
		return nil, errors.New("missing token")
	}
	var raw []byte
	var audience string
	if err := s.db.QueryRowContext(ctx, "SELECT data,audience FROM auth_tokens WHERE "+column+"=? AND expires>?", digest(v), time.Now().Unix()).Scan(&raw, &audience); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if audience != s.resource {
		return nil, errors.New("invalid token audience")
	}
	var t models.Token
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
func (s *sqlStorage) GetByCode(ctx context.Context, v string) (oauth2.TokenInfo, error) {
	return s.token(ctx, "code_hash", v)
}
func (s *sqlStorage) GetByAccess(ctx context.Context, v string) (oauth2.TokenInfo, error) {
	return s.token(ctx, "access_hash", v)
}
func (s *sqlStorage) GetByRefresh(ctx context.Context, v string) (oauth2.TokenInfo, error) {
	return s.token(ctx, "refresh_hash", v)
}
