package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const usage = `Usage: orla <command> [flags]

Commands:
  serve                 Run the server (default)
  attach [id] [-- cmd]  Attach to a terminal; without an id, open a new one here
  ls                    List running terminals
  close <id>            End a terminal and everything it started
  run "prompt"          Run Claude Code on a task and follow its steps
  sessions              List agent sessions
  watch [session]       Follow a session's steps live (default: the latest session)
  hooks                 Show what Claude Code does in Orla terminals (install, uninstall, print)

Run "orla <command> -h" for the flags of a command.
`

func main() {
	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "attach":
		err = attach(args)
	case "ls":
		err = list(args)
	case "close":
		err = closeTerminal(args)
	case "run":
		err = run(args)
	case "sessions":
		err = sessions(args)
	case "watch":
		err = watch(args)
	case "hooks":
		err = hooks(args)
	case "hook":
		err = hook(args)
	case "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "orla:", err)
		os.Exit(1)
	}
}

const defaultAddr = "127.0.0.1:4410"

// defaultDataDir follows the XDG base directory spec: $XDG_DATA_HOME/orla, or
// ~/.local/share/orla.
func defaultDataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "orla")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "orla")
}

func tokenPath(dataDir string) string { return filepath.Join(dataDir, "token") }
