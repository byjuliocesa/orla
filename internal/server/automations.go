package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/byjuliocesa/orla/internal/automation"
	"github.com/byjuliocesa/orla/internal/flow"
	"github.com/byjuliocesa/orla/internal/store"
)

// automationView is an automation and where it runs now.
type automationView struct {
	automation.Automation
	Instances []automation.Status `json:"instances"`
	// On lists where the owner turned it on: "" for global, else session ids.
	On []string `json:"on"`
}

// listAutomations lists every automation, with each place it runs and how it stands there.
func (s *Server) listAutomations(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	on, err := s.store.AutomationsOn(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var statuses []automation.Status
	if s.automations != nil {
		statuses = s.automations.Statuses()
	}
	out := []automationView{}
	for _, a := range automation.List(home) {
		v := automationView{Automation: a, Instances: []automation.Status{}, On: []string{}}
		for _, st := range statuses {
			if st.Name == a.Name {
				v.Instances = append(v.Instances, st)
			}
		}
		for _, o := range on {
			if o.Name == a.Name {
				v.On = append(v.On, o.Session)
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"automations": out})
}

// automationAction turns an automation on (start) or off (stop), globally or for ?session=.
func (s *Server) automationAction(w http.ResponseWriter, r *http.Request) {
	if s.automations == nil {
		writeError(w, http.StatusServiceUnavailable, "Automations are off in this Orla.")
		return
	}
	home, _ := os.UserHomeDir()
	a, ok := automation.Find(home, r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "No such automation.")
		return
	}
	session := r.URL.Query().Get("session")
	if a.Scope == "global" {
		session = ""
	}
	ctx := r.Context()
	switch r.PathValue("action") {
	case "start":
		cwd := ""
		if session != "" {
			x, err := s.store.Session(ctx, session)
			if err != nil {
				writeError(w, http.StatusNotFound, "No such session.")
				return
			}
			cwd = x.Cwd
		}
		if err := s.automations.Start(a, session, cwd); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := s.store.SetAutomationOn(ctx, a.Name, session, true); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	case "stop":
		s.automations.Stop(a.Name, session)
		if err := s.store.SetAutomationOn(ctx, a.Name, session, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	default:
		writeError(w, http.StatusNotFound, "No such action.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// automationLogs returns what an automation printed there, after line ?after=.
func (s *Server) automationLogs(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	lines := []automation.Line{}
	if s.automations != nil {
		lines = s.automations.Lines(r.PathValue("name"), r.URL.Query().Get("session"), after)
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

// deleteAutomation stops an automation everywhere and removes its folder.
func (s *Server) deleteAutomation(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	a, ok := automation.Find(home, r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "No such automation.")
		return
	}
	if s.automations != nil {
		for _, st := range s.automations.Statuses() {
			if st.Name == a.Name {
				s.automations.Stop(st.Name, st.Session)
			}
		}
	}
	dir, err := flow.Unlink(a.Dir)
	if err == nil {
		err = os.RemoveAll(dir)
	}
	err = errors.Join(s.store.DropAutomations(r.Context(), a.Name, ""), err)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ResumeAutomations starts again what was on when Orla stopped.
func ResumeAutomations(ctx context.Context, st *store.Store, m *automation.Manager, log *slog.Logger) {
	home, _ := os.UserHomeDir()
	on, err := st.AutomationsOn(ctx)
	if err != nil {
		log.Warn("reading automations", "err", err)
		return
	}
	for _, o := range on {
		a, ok := automation.Find(home, o.Name)
		if !ok {
			continue
		}
		cwd := ""
		if o.Session != "" {
			x, err := st.Session(ctx, o.Session)
			if err != nil {
				continue
			}
			cwd = x.Cwd
		}
		if err := m.Start(a, o.Session, cwd); err != nil {
			log.Warn("starting automation", "automation", a.Name, "session", o.Session, "err", err)
		}
	}
}
