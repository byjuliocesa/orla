package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Julio0Cesar/orla/internal/events"
)

// Append stores e with the next seq of its session and returns that seq.
//
// The seq is computed inside the INSERT itself: SQLite runs one write at a time, so two appends
// to the same session can never read the same MAX(seq). UNIQUE (session, seq) backs that up.
func (s *Store) Append(ctx context.Context, e events.Event) (int64, error) {
	source, err := json.Marshal(e.Source)
	if err != nil {
		return 0, err
	}
	var data any
	if len(e.Data) > 0 {
		data = string(e.Data)
	}

	var seq int64
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO events (id, session, seq, time, run, parent_run, source, type, data)
		SELECT ?, ?, COALESCE(MAX(seq), 0) + 1, ?, ?, ?, ?, ?, ?
		FROM events WHERE session = ?
		RETURNING seq`,
		e.ID, e.Session, e.Time.UTC().Format(time.RFC3339Nano), e.Run, e.ParentRun,
		string(source), e.Type, data, e.Session,
	).Scan(&seq)
	return seq, err
}

// After returns up to limit events of session with seq greater than after, in seq order.
func (s *Store) After(ctx context.Context, session string, after int64, limit int) ([]events.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, seq, time, run, parent_run, source, type, data
		FROM events WHERE session = ? AND seq > ?
		ORDER BY seq LIMIT ?`, session, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []events.Event
	for rows.Next() {
		var (
			e      = events.Event{Session: session}
			when   string
			source string
			data   *string
		)
		if err := rows.Scan(&e.ID, &e.Seq, &when, &e.Run, &e.ParentRun, &source, &e.Type, &data); err != nil {
			return nil, err
		}
		if e.Time, err = time.Parse(time.RFC3339Nano, when); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(source), &e.Source); err != nil {
			return nil, err
		}
		if data != nil {
			e.Data = json.RawMessage(*data)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LastSeq returns the seq of the session's latest event, 0 if it has none.
func (s *Store) LastSeq(ctx context.Context, session string) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM events WHERE session = ?`, session).Scan(&seq)
	return seq, err
}
