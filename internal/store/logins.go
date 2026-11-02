package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CreateWebLogin records a browser login by the hash of its secret.
func (s *Store) CreateWebLogin(ctx context.Context, hash, agent string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO web_logins (hash, created, agent) VALUES (?, ?, ?)`, hash, formatTime(at), agent)
	return err
}

// WebLoginCreated returns when a login was made, or ErrNotFound.
func (s *Store) WebLoginCreated(ctx context.Context, hash string) (time.Time, error) {
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT created FROM web_logins WHERE hash = ?`, hash).Scan(&created)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, created)
}

func (s *Store) DeleteWebLogin(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM web_logins WHERE hash = ?`, hash)
	return err
}

// DeleteWebLoginsBefore removes logins made before cutoff, which no longer log anyone in.
func (s *Store) DeleteWebLoginsBefore(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM web_logins WHERE created < ?`, formatTime(cutoff))
	return err
}
