package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Preferences shape the interface. They live on the server so they follow
// the owner across devices.
type Preferences struct {
	// Fold is how outlines open: "default" collapses projects and people,
	// "expanded" opens everything, "collapsed" closes everything.
	Fold string `json:"fold"`
	// Theme is "system", "light" or "dark".
	Theme string `json:"theme"`
	// ParseDates turns phrases such as "Friday" or "14h" typed in the
	// interface into due dates and start times.
	ParseDates bool `json:"parse_dates"`
}

// DefaultPreferences apply until the owner changes them.
var DefaultPreferences = Preferences{Fold: "default", Theme: "system", ParseDates: true}

// Preferences returns the stored preferences over the defaults.
func (e *Engine) Preferences(ctx context.Context) (Preferences, error) {
	p := DefaultPreferences
	var raw string
	err := e.db.QueryRowContext(ctx, `SELECT value FROM preferences WHERE key = 'interface'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	// Unknown or missing fields keep their defaults.
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return DefaultPreferences, nil
	}
	return p, nil
}

// SetPreferences validates and stores preferences.
func (e *Engine) SetPreferences(ctx context.Context, p Preferences) (Preferences, error) {
	switch p.Fold {
	case "default", "expanded", "collapsed":
	default:
		return p, invalid("fold is default, expanded or collapsed")
	}
	switch p.Theme {
	case "system", "light", "dark":
	default:
		return p, invalid("theme is system, light or dark")
	}
	b, _ := json.Marshal(p)
	return p, e.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO preferences(key, value) VALUES ('interface', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(b))
		return err
	})
}
