package flow

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed skills/*.md
var own embed.FS

// Install puts Orla's own skills where Claude Code finds them: /newflow, /editflow,
// /deleteflow and /archiveflow, which the AI uses to keep flows in the shape Orla draws. A
// skill of the same name that the owner wrote is left alone.
func Install(home string) error {
	entries, err := own.ReadDir("skills")
	if err != nil {
		return err
	}
	for _, e := range entries {
		body, _ := own.ReadFile("skills/" + e.Name())
		name := strings.TrimSuffix(e.Name(), ".md")
		path := filepath.Join(home, ".claude", "skills", name, "SKILL.md")
		if b, err := os.ReadFile(path); err == nil {
			if Kind(path) != "system" || string(b) == string(body) {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Kind is what Orla marked a skill as in its metadata: "flow", "system" (Orla's own), or "".
func Kind(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	front, _ := split(string(b))
	return front["metadata.orla"]
}
