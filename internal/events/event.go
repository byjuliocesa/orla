package events

import (
	"encoding/json"
	"time"
)

// Engines that can produce events.
const (
	EngineCLI  = "cli"
	EngineLoop = "loop"
)

// Event types. docs/events.md describes the data each one carries.
const (
	RunStarted        = "run.started"
	RunFinished       = "run.finished"
	MessageUser       = "message.user"
	MessageAssistant  = "message.assistant"
	ToolCall          = "tool.call"
	ToolResult        = "tool.result"
	ApprovalRequested = "approval.requested"
	ApprovalResolved  = "approval.resolved"
	Usage             = "usage"
	FilesChanged      = "files.changed"
	Status            = "status"
	Error             = "error"
)

// Source says which engine and agent produced an event.
type Source struct {
	Engine string `json:"engine"`
	Agent  string `json:"agent"`
	// NativeSession is the CLI's own session id, the only handle that resumes it.
	NativeSession string `json:"native_session,omitempty"`
}

// Event is the one format every engine reports in.
type Event struct {
	ID string `json:"id"`
	// Seq is assigned on publish, per session, starting at 1 and without gaps, so a client
	// that reconnects with the last seq it saw misses nothing.
	Seq       int64           `json:"seq"`
	Time      time.Time       `json:"time"`
	Session   string          `json:"session"`
	Run       string          `json:"run,omitempty"`
	ParentRun string          `json:"parent_run,omitempty"`
	Source    Source          `json:"source"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
}
