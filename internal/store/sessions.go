package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type Session struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Cwd   string `json:"cwd"`
	// NativeSession is the CLI's own id for the conversation, set by its first run.
	NativeSession string `json:"native_session,omitempty"`
	// Terminal is set on sessions that started in an Orla terminal, reported by the CLI's hooks.
	Terminal string    `json:"terminal,omitempty"`
	Created  time.Time `json:"created"`
	// Title replaces the folder name in lists when the owner names the session.
	Title    string     `json:"title,omitempty"`
	Pinned   bool       `json:"pinned,omitempty"`
	Archived *time.Time `json:"archived,omitempty"`
	// ForkOf is the CLI conversation this session continues under a new id, until its first run.
	ForkOf string `json:"fork_of,omitempty"`
	// ForkAt is the CLI's id for the last message of ForkOf the fork keeps; empty keeps it all.
	ForkAt string `json:"fork_at,omitempty"`
	// ForkParent and ForkRun are the Orla session and run whose answer a fork goes on from.
	ForkParent string `json:"fork_parent,omitempty"`
	ForkRun    string `json:"fork_run,omitempty"`
	// SkillsOff are the skills this session's conversations leave out.
	SkillsOff []string `json:"skills_off"`
	// Flows are the flow skills this session follows; every other flow is off.
	Flows []string `json:"flows"`
}

// Run statuses. A run is "running" from start until it finishes one of the other ways.
const (
	RunRunning   = "running"
	RunDone      = "done"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

type Run struct {
	ID        string `json:"id"`
	Session   string `json:"session"`
	ParentRun string `json:"parent_run,omitempty"`
	Prompt    string `json:"prompt"`
	Status    string `json:"status"`
	// NativePrompt is the CLI's id for the prompt, linking hook events to their run.
	NativePrompt string     `json:"native_prompt,omitempty"`
	Started      time.Time  `json:"started"`
	Finished     *time.Time `json:"finished,omitempty"`
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func (s *Store) CreateSession(ctx context.Context, x Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, agent, cwd, native_session, terminal, created, title, fork_of, fork_at, fork_parent, fork_run) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		x.ID, x.Agent, x.Cwd, x.NativeSession, x.Terminal, formatTime(x.Created), x.Title, x.ForkOf, x.ForkAt, x.ForkParent, x.ForkRun)
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
		`SELECT id, agent, cwd, native_session, terminal, created, title, pinned, archived, fork_of, fork_at, fork_parent, fork_run, skills_off, flows FROM sessions `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var x Session
		var created string
		var archived sql.NullString
		var skillsOff, flows string
		if err := rows.Scan(&x.ID, &x.Agent, &x.Cwd, &x.NativeSession, &x.Terminal, &created,
			&x.Title, &x.Pinned, &archived, &x.ForkOf, &x.ForkAt, &x.ForkParent, &x.ForkRun, &skillsOff, &flows); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(skillsOff), &x.SkillsOff)
		_ = json.Unmarshal([]byte(flows), &x.Flows)
		if x.Flows == nil {
			x.Flows = []string{}
		}
		if x.SkillsOff == nil {
			x.SkillsOff = []string{}
		}
		x.Created, _ = time.Parse(time.RFC3339Nano, created)
		if archived.Valid {
			t, _ := time.Parse(time.RFC3339Nano, archived.String)
			x.Archived = &t
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// LatestLimits is the newest report of the account's plan windows, from any session, with
// when it came. ok is false when no run has reported one yet.
func (s *Store) LatestLimits(ctx context.Context) (limits []byte, at time.Time, ok bool, err error) {
	var data, when string
	err = s.db.QueryRowContext(ctx,
		`SELECT data, time FROM events WHERE type = 'usage' AND data LIKE '%"scope":"account"%' ORDER BY time DESC LIMIT 1`).Scan(&data, &when)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}
	at, _ = time.Parse(time.RFC3339Nano, when)
	return []byte(data), at, true, nil
}

func (s *Store) SetNativeSession(ctx context.Context, id, native string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET native_session = ? WHERE id = ?`, native, id)
	return err
}

func (s *Store) CreateRun(ctx context.Context, r Run) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (id, session, parent_run, prompt, status, native_prompt, started)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Session, r.ParentRun, r.Prompt, r.Status, r.NativePrompt, formatTime(r.Started))
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
		`SELECT id, session, parent_run, prompt, status, native_prompt, started, finished FROM runs `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		var started string
		var finished sql.NullString
		if err := rows.Scan(&r.ID, &r.Session, &r.ParentRun, &r.Prompt, &r.Status, &r.NativePrompt,
			&started, &finished); err != nil {
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

// SessionByNative finds the session that holds a CLI conversation.
func (s *Store) SessionByNative(ctx context.Context, agent, native string) (Session, error) {
	rows, err := s.sessions(ctx, `WHERE agent = ? AND native_session = ? ORDER BY id LIMIT 1`, agent, native)
	if err != nil {
		return Session{}, err
	}
	if len(rows) == 0 {
		return Session{}, ErrNotFound
	}
	return rows[0], nil
}

// SessionsOfTerminal returns the sessions that started in a terminal.
func (s *Store) SessionsOfTerminal(ctx context.Context, terminal string) ([]Session, error) {
	return s.sessions(ctx, `WHERE terminal = ?`, terminal)
}

// RunByNativePrompt finds a session's run for a CLI prompt id.
func (s *Store) RunByNativePrompt(ctx context.Context, session, prompt string) (Run, error) {
	rows, err := s.runs(ctx, `WHERE session = ? AND native_prompt = ? LIMIT 1`, session, prompt)
	if err != nil {
		return Run{}, err
	}
	if len(rows) == 0 {
		return Run{}, ErrNotFound
	}
	return rows[0], nil
}

// RunningRunsOf returns a session's unfinished runs.
func (s *Store) RunningRunsOf(ctx context.Context, session string) ([]Run, error) {
	return s.runs(ctx, `WHERE session = ? AND status = ?`, session, RunRunning)
}

func (s *Store) SetSessionTerminal(ctx context.Context, id, terminal string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET terminal = ? WHERE id = ?`, terminal, id)
	return err
}

func (s *Store) SetSessionTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET title = ? WHERE id = ?`, title, id)
	return err
}

// SetSessionSkillsOff sets the skills a session leaves out.
func (s *Store) SetSessionSkillsOff(ctx context.Context, id string, names []string) error {
	if names == nil {
		names = []string{}
	}
	b, _ := json.Marshal(names)
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET skills_off = ? WHERE id = ?`, string(b), id)
	return err
}

// SetSessionFlows sets the flows a session follows.
func (s *Store) SetSessionFlows(ctx context.Context, id string, names []string) error {
	if names == nil {
		names = []string{}
	}
	b, _ := json.Marshal(names)
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET flows = ? WHERE id = ?`, string(b), id)
	return err
}

func (s *Store) SetSessionPinned(ctx context.Context, id string, pinned bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET pinned = ? WHERE id = ?`, pinned, id)
	return err
}

// SetSessionArchived archives the session at the given time, or brings it back with nil.
func (s *Store) SetSessionArchived(ctx context.Context, id string, at *time.Time) error {
	var v any
	if at != nil {
		v = formatTime(*at)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET archived = ? WHERE id = ?`, v, id)
	return err
}

// DeleteSession removes a session with its runs and events. Its folder and worktree stay on disk.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM events WHERE session = ?`,
		`DELETE FROM runs WHERE session = ?`,
		`DELETE FROM sessions WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
