package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// InstallCommand installs Claude Code for the user running Orla, with no root needed.
const InstallCommand = "curl -fsSL https://claude.ai/install.sh | bash"

// Status is whether the agent can run on this machine.
type Status struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	LoggedIn  bool   `json:"loggedIn"`
	Account   string `json:"account,omitempty"`
}

// exe is the executable to run. A bare name not on PATH falls back to where the installer puts
// it: a server started before the install, or with a short PATH, still finds it.
func (c Claude) exe() string {
	path := c.Path
	if path == "" {
		path = "claude"
	}
	if strings.ContainsRune(path, filepath.Separator) {
		return path
	}
	if _, err := exec.LookPath(path); err == nil {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, p := range []string{filepath.Join(home, ".local", "bin", path), filepath.Join(home, ".claude", "local", path)} {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				return p
			}
		}
	}
	return path
}

// Exe is the executable Orla runs, for a terminal that should run the same one.
func (c Claude) Exe() string { return c.exe() }

// Status asks the CLI for its version and whether it is signed in.
func (c Claude) Status(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	path := c.exe()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = headlessEnv()
	out, err := cmd.Output()
	if err != nil {
		return Status{}
	}
	s := Status{Installed: true, Version: strings.Fields(string(out) + " ")[0]}
	cmd = exec.CommandContext(ctx, path, "auth", "status")
	cmd.Env = headlessEnv()
	out, _ = cmd.Output() // it exits 1 when signed out, still printing the JSON
	var auth struct {
		LoggedIn bool   `json:"loggedIn"`
		Email    string `json:"email"`
	}
	if json.Unmarshal(out, &auth) == nil {
		s.LoggedIn = auth.LoggedIn
		s.Account = auth.Email
	}
	return s
}
