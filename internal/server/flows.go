package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/byjuliocesa/orla/internal/flow"
)

// flows lists every flow Claude Code would see from ?cwd=, the owner's and the project's, read
// fresh each time: the AI rewrites them as it learns, and the page follows.
func (s *Server) flows(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	cwd := r.URL.Query().Get("cwd")
	if !filepath.IsAbs(cwd) {
		cwd = ""
	}
	out := flow.List(home, cwd)
	if out == nil {
		out = []flow.Flow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"flows": out})
}

// skillDoc returns a skill's SKILL.md, the owner's or the project's, found by its name, so a
// step can show the skills it uses.
func (s *Server) skillDoc(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	name := r.URL.Query().Get("name")
	cwd := r.URL.Query().Get("cwd")
	dirs := []string{filepath.Join(home, ".claude", "skills")}
	if filepath.IsAbs(cwd) {
		dirs = append(dirs, filepath.Join(cwd, ".claude", "skills"))
	}
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			p := filepath.Join(dir, e.Name(), "SKILL.md")
			n, _ := skillHead(p)
			if n != name {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil || len(b) > 256<<10 {
				continue
			}
			writeJSON(w, http.StatusOK, map[string]any{"name": n, "path": p, "body": string(b)})
			return
		}
	}
	writeError(w, http.StatusNotFound, "That skill has no SKILL.md Orla can read; it may come with Claude Code or a plugin.")
}

// archivedFlows lists what /archiveflow took out: whole flows, and steps of flows.
func (s *Server) archivedFlows(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	flows, steps := flow.Archived(home)
	writeJSON(w, http.StatusOK, map[string]any{"flows": flows, "steps": steps})
}

// flowAction archives, restores or deletes a flow or one of its steps, from Orla's buttons.
func (s *Server) flowAction(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	cwd := r.URL.Query().Get("cwd")
	if !filepath.IsAbs(cwd) {
		cwd = ""
	}
	name, step, action := r.PathValue("name"), r.PathValue("step"), r.PathValue("action")
	if r.Method == http.MethodDelete {
		action = "delete"
	}
	var err error
	copied := ""
	switch {
	case action == "duplicate" && step == "":
		copied, err = flow.DuplicateFlow(home, cwd, name)
	case action == "archive" && step == "":
		err = flow.ArchiveFlow(home, cwd, name)
	case action == "restore" && step == "":
		err = flow.RestoreFlow(home, name)
	case action == "delete" && step == "":
		err = flow.DeleteFlow(home, cwd, name)
	case action == "archive":
		err = flow.ArchiveStep(home, cwd, name, step)
	case action == "restore":
		err = flow.RestoreStep(home, cwd, name, step)
	case action == "delete":
		err = flow.DeleteStep(home, cwd, name, step)
	default:
		writeError(w, http.StatusNotFound, "No such action.")
		return
	}
	if errors.Is(err, flow.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if copied != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": copied})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
