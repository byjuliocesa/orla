package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// LatestRun returns a session's most recent run.
func (s *Store) LatestRun(ctx context.Context, session string) (Run, error) {
	rows, err := s.runs(ctx, `WHERE session = ? ORDER BY started DESC, id DESC LIMIT 1`, session)
	if err != nil {
		return Run{}, err
	}
	if len(rows) == 0 {
		return Run{}, ErrNotFound
	}
	return rows[0], nil
}

// Activity is what a session list needs to know about a session's events.
type Activity struct {
	LastSeq int64
	// Updated is when the latest event happened; zero without events.
	Updated time.Time
	// State is the latest status event's state: idle, working, waiting or ended. Empty when
	// the session never reported one (headless runs report runs, not statuses).
	State string
}

func (s *Store) Activity(ctx context.Context, session string) (Activity, error) {
	var a Activity
	var updated sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT seq, time FROM events WHERE session = ? ORDER BY seq DESC LIMIT 1`, session).
		Scan(&a.LastSeq, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	a.Updated, _ = time.Parse(time.RFC3339Nano, updated.String)
	var state sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT json_extract(data, '$.state') FROM events
		WHERE session = ? AND type = 'status' ORDER BY seq DESC LIMIT 1`, session).Scan(&state)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return a, err
	}
	a.State = state.String
	return a, nil
}
