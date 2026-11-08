package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Pref returns the JSON kept under key, or nil when nothing is.
func (s *Store) Pref(ctx context.Context, key string) (json.RawMessage, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM prefs WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(v), nil
}

// SetPref keeps value under key, replacing what was there.
func (s *Store) SetPref(ctx context.Context, key string, value json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO prefs (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, string(value))
	return err
}
