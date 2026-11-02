package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Julio0Cesar/orla/internal/engine/cli"
)

// hookTimeout bounds how long Claude Code waits on Orla per hook: PreToolUse holds the tool
// until the hook returns, so a slow or dead server must not stall the agent.
const hookTimeout = 2 * time.Second

// hook is the command Claude Code runs for each hook event. Outside an Orla terminal it does
// nothing, so it can sit in the global Claude Code settings. It never fails, and it prints only
// the owner's answer to a permission prompt: a failing hook shows errors in the CLI, and
// UserPromptSubmit adds stdout to the prompt.
func hook(_ []string) error {
	sendHook(os.Stdin, os.Stdout, os.Getenv)
	return nil
}

func sendHook(stdin io.Reader, stdout io.Writer, getenv func(string) string) {
	terminal, url, token := getenv("ORLA_TERMINAL"), getenv("ORLA_HOOK_URL"), getenv("ORLA_HOOK_TOKEN")
	if terminal == "" || url == "" || token == "" {
		_, _ = io.Copy(io.Discard, stdin)
		return
	}
	body, err := io.ReadAll(io.LimitReader(stdin, 8<<20))
	if err != nil {
		return
	}
	var h struct {
		Event string `json:"hook_event_name"`
	}
	_ = json.Unmarshal(body, &h)
	// A permission prompt waits for the owner. The terminal shows it meanwhile, so the wait
	// holds nobody up.
	asks := h.Event == cli.HookPermissionRequest
	timeout := hookTimeout
	if asks {
		timeout = cli.PermissionHookTimeout
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Orla-Terminal", terminal)
	res, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	if !asks || res.StatusCode != http.StatusOK {
		return
	}
	answer, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil || !json.Valid(answer) {
		return
	}
	_, _ = stdout.Write(answer)
}

func hookCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	return strconv.Quote(exe) + " hook", nil
}

// isOrlaHook recognizes the entries hooks install writes, so uninstall removes those and only
// those, wherever the binary has moved since.
func isOrlaHook(command string) bool {
	command = strings.TrimSpace(command)
	if !strings.HasSuffix(command, " hook") {
		return false
	}
	exe := strings.Trim(strings.TrimSuffix(command, " hook"), `"`)
	return filepath.Base(exe) == "orla"
}

func hooks(args []string) error {
	fs := flag.NewFlagSet("hooks", flag.ExitOnError)
	home, _ := os.UserHomeDir()
	file := fs.String("file", filepath.Join(home, ".claude", "settings.json"), "Claude Code settings file")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: orla hooks print|install|uninstall [-file settings.json]

  print      Print the hooks as a settings fragment, for claude --settings
  install    Add them to the Claude Code settings file, keeping everything else
  uninstall  Remove them from it

The hooks report what Claude Code does in Orla terminals and bring its permission prompts to
you, wherever you are; the terminal keeps asking too. Anywhere else they do nothing.
`)
	}
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	action := args[0]
	_ = fs.Parse(args[1:])

	command, err := hookCommand()
	if err != nil {
		return err
	}
	switch action {
	case "print":
		b, _ := json.MarshalIndent(cli.HookSettings(command), "", "  ")
		fmt.Println(string(b))
		return nil
	case "install", "uninstall":
		return editSettings(*file, command, action == "install")
	}
	fs.Usage()
	os.Exit(2)
	return nil
}

// editSettings adds or removes Orla's hooks in a Claude Code settings file. Other settings and
// other hooks are kept; the previous file is saved next to it first.
func editSettings(path, command string, install bool) error {
	settings := map[string]any{}
	before, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		before = nil
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(before, &settings); err != nil {
			return fmt.Errorf("%s is not valid JSON, so it is left alone: %w", path, err)
		}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	// Remove ours first, so install is idempotent and picks up a moved binary.
	for event, raw := range hooks {
		entries, _ := raw.([]any)
		var kept []any
		for _, e := range entries {
			entry, _ := e.(map[string]any)
			inner, _ := entry["hooks"].([]any)
			var keptInner []any
			for _, h := range inner {
				hm, _ := h.(map[string]any)
				if c, _ := hm["command"].(string); isOrlaHook(c) {
					continue
				}
				keptInner = append(keptInner, h)
			}
			if len(keptInner) > 0 {
				entry["hooks"] = keptInner
				kept = append(kept, entry)
			}
		}
		if len(kept) > 0 {
			hooks[event] = kept
		} else {
			delete(hooks, event)
		}
	}
	if install {
		ours := cli.HookSettings(command)["hooks"].(map[string]any)
		for event, entries := range ours {
			existing, _ := hooks[event].([]any)
			hooks[event] = append(existing, entries.([]any)...)
		}
	}
	if len(hooks) > 0 {
		settings["hooks"] = hooks
	} else {
		delete(settings, "hooks")
	}

	after, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	after = append(after, '\n')
	if bytes.Equal(before, after) {
		fmt.Println("nothing to change in", path)
		return nil
	}
	if before != nil {
		backup := path + ".orla-backup"
		if err := os.WriteFile(backup, before, 0o600); err != nil {
			return err
		}
		fmt.Println("previous settings saved to", backup)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Written beside and renamed over, so a crash never leaves Claude Code half a settings file.
	tmp := path + ".orla-tmp"
	if err := os.WriteFile(tmp, after, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if install {
		fmt.Println("hooks installed in", path)
	} else {
		fmt.Println("hooks removed from", path)
	}
	return nil
}
