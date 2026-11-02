package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type Session struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Cwd   string `json:"cwd"`
	// NativeSession is the CLI's own id for the conversation, set by its first run.
	NativeSession string    `json:"native_session,omitempty"`
	Created       time.Time `json:"created"`
}

// Run statuses. A run is "running" from start until it finishes one of the other ways.
const (
	RunRunning   = "running"
	RunDone      = "done"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

type Run struct {
	ID        string     `json:"id"`
	Session   string     `json:"session"`
	ParentRun string     `json:"parent_run,omitempty"`
	Prompt    string     `json:"prompt"`
	Status    string     `json:"status"`
	Started   time.Time  `json:"started"`
	Finished  *time.Time `json:"finished,omitempty"`
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func (s *Store) CreateSession(ctx context.Context, x Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, agent, cwd, native_session, created) VALUES (?, ?, ?, ?, ?)`,
		x.ID, x.Agent, x.Cwd, x.NativeSession, formatTime(x.Created))
	return err
}

func (s *Store) Session(ctx context.Context, id string) (Session, error) {
	rows, err := s.sessions(ctx, `WHERE id = ?`, id)
	if err != nil {
		return Session{}, err
	}
	if len(rows) == 0 {
		return Session{}, ErrNotFound
	}
	return rows[0], nil
}

// Sessions returns every session, newest first.
func (s *Store) Sessions(ctx context.Context) ([]Session, error) {
	return s.sessions(ctx, `ORDER BY id DESC`)
}

func (s *Store) sessions(ctx context.Context, where string, args ...any) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent, cwd, native_session, created FROM sessions `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var x Session
		var created string
		if err := rows.Scan(&x.ID, &x.Agent, &x.Cwd, &x.NativeSession, &created); err != nil {
			return nil, err
		}
		x.Created, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) SetNativeSession(ctx context.Context, id, native string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET native_session = ? WHERE id = ?`, native, id)
	return err
}

func (s *Store) CreateRun(ctx context.Context, r Run) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (id, session, parent_run, prompt, status, started) VALUES (?, ?, ?, ?, ?, ?)`,
		r.ID, r.Session, r.ParentRun, r.Prompt, r.Status, formatTime(r.Started))
	return err
}

func (s *Store) FinishRun(ctx context.Context, id, status string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET status = ?, finished = ? WHERE id = ?`, status, formatTime(at), id)
	return err
}

// Runs returns a session's runs, oldest first.
func (s *Store) Runs(ctx context.Context, session string) ([]Run, error) {
	return s.runs(ctx, `WHERE session = ? ORDER BY started, id`, session)
}

// RunningRuns returns runs not yet finished. After a restart these are runs whose process died
// with the server.
func (s *Store) RunningRuns(ctx context.Context) ([]Run, error) {
	return s.runs(ctx, `WHERE status = ?`, RunRunning)
}

func (s *Store) runs(ctx context.Context, where string, args ...any) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session, parent_run, prompt, status, started, finished FROM runs `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		var started string
		var finished sql.NullString
		if err := rows.Scan(&r.ID, &r.Session, &r.ParentRun, &r.Prompt, &r.Status, &started, &finished); err != nil {
			return nil, err
		}
		r.Started, _ = time.Parse(time.RFC3339Nano, started)
		if finished.Valid {
			t, _ := time.Parse(time.RFC3339Nano, finished.String)
			r.Finished = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
