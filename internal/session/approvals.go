package session

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/Julio0Cesar/orla/internal/events"
	"github.com/Julio0Cesar/orla/internal/id"
	"github.com/Julio0Cesar/orla/internal/store"
)

// defaultApprovalWait is how long a request waits for the owner. A headless run is paused
// meanwhile, holding its subscription window and the session; then the request is denied. An
// interactive one is withdrawn instead, and the terminal goes on asking.
const defaultApprovalWait = 10 * time.Minute

var ErrNoApproval = errors.New("no such pending approval")

// Decision is the owner's answer to an approval request.
type Decision struct {
	Allow   bool   `json:"allow"`
	Message string `json:"message,omitempty"`
	// By says where the owner answered: "cli", "web", "whatsapp". Endings that are not an
	// answer through Orla say "timeout", "cancel", "run" or "terminal".
	By string `json:"by,omitempty"`
}

type pending struct {
	sess    store.Session
	run     string
	callID  string
	tool    string
	input   json.RawMessage
	asked   time.Time
	expires time.Time
	// terminal is set when the CLI asks in an Orla terminal, which shows the same prompt.
	terminal string
	// ended receives the one ending of the request.
	ended chan ending
}

// ending is how a request ended.
type ending struct {
	Decision
	// fromOwner: the owner answered through Orla. Otherwise it timed out, its run ended, the
	// CLI stopped waiting or the prompt was answered in the terminal.
	fromOwner bool
	// withdrawn: nobody decided through Orla, and the terminal still asks.
	withdrawn bool
}

func denial(by, message string) ending {
	return ending{Decision: Decision{Allow: false, Message: message, By: by}}
}

func withdrawal(by, message string) ending {
	return ending{Decision: Decision{Allow: false, Message: message, By: by}, withdrawn: true}
}

// RequestApproval asks the owner whether a headless run may use a tool, and waits for the
// answer. It is what the CLI's permission prompt tool calls. The request is denied when the
// owner does not answer in time, the run ends, or the CLI stops waiting (ctx).
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

	record := context.WithoutCancel(ctx)
	p := &pending{sess: sess, run: run, callID: callID, ended: make(chan ending, 1)}
	approvalID := s.openApproval(record, p, tool, input)
	select {
	case e := <-p.ended:
		return e.Decision
	case <-time.After(s.approvalWait):
		s.endApproval(record, approvalID, nil, denial("timeout", "no answer from the owner in time"))
	case <-ar.ctx.Done():
		s.endApproval(record, approvalID, nil, denial("cancel", "the run was cancelled"))
	case <-ctx.Done():
		s.endApproval(record, approvalID, nil, denial("cancel", "the CLI stopped waiting"))
	}
	// Whichever ending came first holds, even an answer that raced the timeout.
	return (<-p.ended).Decision
}

// Resolve answers a pending approval of a session.
func (s *Service) Resolve(ctx context.Context, session, approvalID string, d Decision) error {
	ofSession := func(p *pending) bool { return p.sess.ID == session }
	if !s.endApproval(context.WithoutCancel(ctx), approvalID, ofSession, ending{Decision: d, fromOwner: true}) {
		return ErrNoApproval
	}
	return nil
}

// openApproval records a request and announces it.
func (s *Service) openApproval(ctx context.Context, p *pending, tool string, input json.RawMessage) string {
	approvalID := id.New("apr")
	p.tool, p.input = tool, input
	p.asked = time.Now().UTC()
	p.expires = p.asked.Add(s.approvalWait)
	s.mu.Lock()
	s.approvals[approvalID] = p
	s.mu.Unlock()

	data := map[string]any{
		"approval_id": approvalID, "tool": tool, "input": input, "call_id": p.callID,
		"expires_at": p.expires,
	}
	if p.terminal != "" {
		data["terminal"] = p.terminal
	}
	s.publish(ctx, p.sess, p.run, events.ApprovalRequested, data)
	return approvalID
}

// endApproval ends a pending request, if it is still pending and match accepts it. Each request
// is taken out at once, so of endings that race only the first counts.
func (s *Service) endApproval(ctx context.Context, approvalID string, match func(*pending) bool, e ending) bool {
	s.mu.Lock()
	p, ok := s.approvals[approvalID]
	if ok && match != nil && !match(p) {
		ok = false
	}
	if ok {
		delete(s.approvals, approvalID)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	data := map[string]any{"approval_id": approvalID, "allowed": e.Allow, "by": e.By, "message": e.Message}
	if e.withdrawn {
		data["withdrawn"] = true
	}
	s.publish(ctx, p.sess, p.run, events.ApprovalResolved, data)
	p.ended <- e
	return true
}

// endApprovalsWhere ends every pending request match accepts.
func (s *Service) endApprovalsWhere(ctx context.Context, match func(*pending) bool, e ending) {
	s.mu.Lock()
	var ids []string
	for approvalID, p := range s.approvals {
		if match(p) {
			ids = append(ids, approvalID)
		}
	}
	s.mu.Unlock()
	for _, approvalID := range ids {
		s.endApproval(ctx, approvalID, match, e)
	}
}

// PendingApproval is a request waiting for the owner.
type PendingApproval struct {
	ID       string          `json:"approval_id"`
	Session  string          `json:"session"`
	Run      string          `json:"run"`
	Tool     string          `json:"tool"`
	Input    json.RawMessage `json:"input"`
	CallID   string          `json:"call_id,omitempty"`
	Terminal string          `json:"terminal,omitempty"`
	Asked    time.Time       `json:"asked_at"`
	Expires  time.Time       `json:"expires_at"`
}

// PendingApprovals returns every request waiting for the owner, oldest first.
func (s *Service) PendingApprovals() []PendingApproval {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []PendingApproval{}
	for approvalID, p := range s.approvals {
		out = append(out, PendingApproval{
			ID: approvalID, Session: p.sess.ID, Run: p.run, Tool: p.tool, Input: p.input,
			CallID: p.callID, Terminal: p.terminal, Asked: p.asked, Expires: p.expires,
		})
	}
	slices.SortFunc(out, func(a, b PendingApproval) int { return a.Asked.Compare(b.Asked) })
	return out
}

// PendingCount is how many requests of a session wait for the owner.
func (s *Service) PendingCount(session string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.approvals {
		if p.sess.ID == session {
			n++
		}
	}
	return n
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
