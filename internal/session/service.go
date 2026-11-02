package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/Julio0Cesar/orla/internal/engine/cli"
	"github.com/Julio0Cesar/orla/internal/events"
	"github.com/Julio0Cesar/orla/internal/id"
	"github.com/Julio0Cesar/orla/internal/store"
)

var (
	// ErrBusy: a CLI conversation runs one prompt at a time, so a session takes one run at a
	// time too.
	ErrBusy         = errors.New("session already has a run in progress")
	ErrUnknownAgent = errors.New("unknown agent")
	ErrNotRunning   = errors.New("session has no run in progress")
)

// Agents that can back a session.
var Agents = []string{"claude"}

// Runner runs one headless CLI run. cli.Claude is one.
type Runner interface {
	Run(ctx context.Context, spec cli.Spec, emit func(cli.Out), native func(string)) cli.Outcome
}

// Service owns sessions and their runs.
type Service struct {
	log     *slog.Logger
	store   *store.Store
	bus     *events.Bus
	runners map[string]Runner

	// ctx outlives any request: a run goes on after the client that started it leaves.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.Mutex
	active map[string]*active // by session id
}

type active struct {
	run    string
	cancel context.CancelFunc
}

func NewService(log *slog.Logger, st *store.Store, bus *events.Bus, runners map[string]Runner) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		log: log, store: st, bus: bus, runners: runners,
		ctx: ctx, cancel: cancel, active: map[string]*active{},
	}
}

// Recover closes runs left "running" by a previous process. Their CLI died with it, so they
// end as failed, with the run.finished event clients wait for.
func (s *Service) Recover(ctx context.Context) error {
	runs, err := s.store.RunningRuns(ctx)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if n := cli.KillLeftovers(r.ID); n > 0 {
			s.log.Warn("killed processes left by a run interrupted by a crash", "run", r.ID, "count", n)
		}
		sess, err := s.store.Session(ctx, r.Session)
		if err != nil {
			return err
		}
		s.publish(ctx, sess, r.ID, events.RunFinished, map[string]any{
			"status": store.RunFailed, "summary": "the server stopped while this run was in progress",
		})
		if err := s.store.FinishRun(ctx, r.ID, store.RunFailed, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Create(ctx context.Context, agent, cwd string) (store.Session, error) {
	if !slices.Contains(Agents, agent) || s.runners[agent] == nil {
		return store.Session{}, fmt.Errorf("%w: %q", ErrUnknownAgent, agent)
	}
	sess := store.Session{ID: id.New("ses"), Agent: agent, Cwd: cwd, Created: time.Now().UTC()}
	return sess, s.store.CreateSession(ctx, sess)
}

// Busy reports the run in progress in a session, if any.
func (s *Service) Busy(session string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.active[session]
	if !ok {
		return "", false
	}
	return a.run, true
}

type RunOptions struct {
	Prompt         string
	PermissionMode string
	Model          string
	ParentRun      string
}

// Start begins a run in the background and returns its id. Its progress is published as
// events on the session.
func (s *Service) Start(ctx context.Context, sessionID string, opts RunOptions) (store.Run, error) {
	sess, err := s.store.Session(ctx, sessionID)
	if err != nil {
		return store.Run{}, err
	}
	runner := s.runners[sess.Agent]
	if runner == nil {
		return store.Run{}, fmt.Errorf("%w: %q", ErrUnknownAgent, sess.Agent)
	}

	run := store.Run{
		ID: id.New("run"), Session: sess.ID, ParentRun: opts.ParentRun, Prompt: opts.Prompt,
		Status: store.RunRunning, Started: time.Now().UTC(),
	}
	runCtx, cancel := context.WithCancel(s.ctx)

	// Claimed before anything is stored, so two concurrent starts cannot both get through.
	s.mu.Lock()
	if _, busy := s.active[sess.ID]; busy {
		s.mu.Unlock()
		cancel()
		return store.Run{}, ErrBusy
	}
	s.active[sess.ID] = &active{run: run.ID, cancel: cancel}
	s.mu.Unlock()

	release := func() {
		s.mu.Lock()
		delete(s.active, sess.ID)
		s.mu.Unlock()
		cancel()
	}
	if err := s.store.CreateRun(ctx, run); err != nil {
		release()
		return store.Run{}, err
	}
	s.publish(ctx, sess, run.ID, events.RunStarted, map[string]any{
		"prompt": opts.Prompt, "cwd": sess.Cwd, "permission_mode": opts.PermissionMode,
		"resumes": sess.NativeSession,
	})

	s.wg.Go(func() {
		defer release()
		spec := cli.Spec{
			Run: run.ID, Cwd: sess.Cwd, Prompt: opts.Prompt, Resume: sess.NativeSession,
			PermissionMode: opts.PermissionMode, Model: opts.Model,
		}
		// Recording is never cancelled: a run stopped midway, or by shutdown, must still store
		// what it did and how it ended.
		record := context.WithoutCancel(runCtx)
		outcome := runner.Run(runCtx, spec,
			func(out cli.Out) { s.publish(record, sess, run.ID, out.Type, out.Data) },
			func(native string) {
				sess.NativeSession = native
				if err := s.store.SetNativeSession(record, sess.ID, native); err != nil {
					s.log.Error("saving native session", "session", sess.ID, "err", err)
				}
			})

		s.publish(record, sess, run.ID, events.RunFinished, map[string]any{
			"status": outcome.Status, "summary": outcome.Summary,
		})
		if err := s.store.FinishRun(record, run.ID, outcome.Status, time.Now()); err != nil {
			s.log.Error("finishing run", "run", run.ID, "err", err)
		}
	})
	return run, nil
}

// Cancel stops the run in progress in a session. It returns once the stop is requested; the
// run.finished event says when it is over.
func (s *Service) Cancel(session string) error {
	s.mu.Lock()
	a, ok := s.active[session]
	s.mu.Unlock()
	if !ok {
		return ErrNotRunning
	}
	a.cancel()
	return nil
}

// Close cancels every run and waits for them to record how they ended.
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
}

func (s *Service) publish(ctx context.Context, sess store.Session, run, typ string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		s.log.Error("encoding event", "type", typ, "err", err)
		return
	}
	_, err = s.bus.Publish(ctx, events.Event{
		Session: sess.ID,
		Run:     run,
		Source:  events.Source{Engine: events.EngineCLI, Agent: sess.Agent, NativeSession: sess.NativeSession},
		Type:    typ,
		Data:    raw,
	})
	if err != nil {
		s.log.Error("publishing event", "type", typ, "session", sess.ID, "err", err)
	}
}
