package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Julio0Cesar/orla/internal/events"
	"github.com/Julio0Cesar/orla/internal/id"
	"github.com/Julio0Cesar/orla/internal/store"
)

// defaultApprovalWait is how long a run waits for the owner before a request is denied. The CLI is
// paused meanwhile, holding its subscription window and the session.
const defaultApprovalWait = 10 * time.Minute

var (
	ErrNoApproval       = errors.New("no such pending approval")
	ErrAlreadyDecided   = errors.New("approval already decided")
	ErrUnknownRunSecret = errors.New("unknown run or secret")
)

// Decision is the owner's answer to an approval request.
type Decision struct {
	Allow   bool   `json:"allow"`
	Message string `json:"message,omitempty"`
	// By says where the owner answered: "cli", "web", "whatsapp".
	By string `json:"by,omitempty"`
}

type pending struct {
	session string
	run     string
	answer  chan Decision
}

// RequestApproval asks the owner whether a run may use a tool, and waits for the answer. It
// is what the CLI's permission prompt tool calls. The request is denied when the owner does not
// answer in time, the run ends, or the CLI stops waiting (ctx).
func (s *Service) RequestApproval(ctx context.Context, run, tool string, input json.RawMessage, callID string) Decision {
	s.mu.Lock()
	ar, ok := s.runSecrets[run]
	s.mu.Unlock()
	if !ok {
		return Decision{Allow: false, Message: "this run is not in progress"}
	}
	sess, err := s.store.Session(ctx, ar.session)
	if err != nil {
		return Decision{Allow: false, Message: "session not found"}
	}

	p := &pending{session: sess.ID, run: run, answer: make(chan Decision, 1)}
	approvalID := id.New("apr")
	s.mu.Lock()
	s.approvals[approvalID] = p
	s.mu.Unlock()

	record := context.WithoutCancel(ctx)
	s.publish(record, sess, run, events.ApprovalRequested, map[string]any{
		"approval_id": approvalID, "tool": tool, "input": input, "call_id": callID,
		"expires_at": time.Now().Add(s.approvalWait).UTC(),
	})

	var d Decision
	answered := false
	select {
	case d = <-p.answer:
		answered = true
	case <-time.After(s.approvalWait):
		d = Decision{Allow: false, Message: "no answer from the owner in time", By: "timeout"}
	case <-ar.ctx.Done():
		d = Decision{Allow: false, Message: "the run was cancelled", By: "cancel"}
	case <-ctx.Done():
		d = Decision{Allow: false, Message: "the CLI stopped waiting", By: "cancel"}
	}

	if answered {
		return d // Resolve took the request out and published the answer
	}
	s.mu.Lock()
	_, unanswered := s.approvals[approvalID]
	delete(s.approvals, approvalID)
	s.mu.Unlock()
	if !unanswered {
		// The owner answered as time ran out; Resolve took the request and published that
		// answer, so it is the one that holds.
		return <-p.answer
	}
	s.publish(record, sess, run, events.ApprovalResolved, map[string]any{
		"approval_id": approvalID, "allowed": false, "by": d.By, "message": d.Message,
	})
	return d
}

// Resolve answers a pending approval of a session.
func (s *Service) Resolve(ctx context.Context, session, approvalID string, d Decision) error {
	s.mu.Lock()
	p, ok := s.approvals[approvalID]
	if ok && p.session != session {
		ok = false
	}
	if ok {
		// Taken out at once, so of two answers racing only the first counts.
		delete(s.approvals, approvalID)
	}
	s.mu.Unlock()
	if !ok {
		return ErrNoApproval
	}
	sess, err := s.store.Session(ctx, session)
	if err != nil {
		return err
	}
	s.publish(context.WithoutCancel(ctx), sess, p.run, events.ApprovalResolved, map[string]any{
		"approval_id": approvalID, "allowed": d.Allow, "by": d.By, "message": d.Message,
	})
	p.answer <- d
	return nil
}

// runAccess is what a run's MCP endpoint is checked against.
type runAccess struct {
	session string
	secret  string
	ctx     context.Context
}

// RunAccess returns the secret a run's MCP client must present, if the run is in progress.
func (s *Service) RunAccess(run string) (secret string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ar, ok := s.runSecrets[run]
	return ar.secret, ok
}

func (s *Service) storeRunAccess(run string, sess store.Session, secret string, ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runSecrets[run] = runAccess{session: sess.ID, secret: secret, ctx: ctx}
}

func (s *Service) dropRunAccess(run string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runSecrets, run)
}
