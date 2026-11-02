package terminal

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// signalSession signals every process in session sid.
//
// The process group is not enough: an interactive shell with job control puts each background
// job in a group of its own, so signalling the shell's group leaves those jobs running. Every
// process started from the terminal stays in its session unless it calls setsid itself, which
// is the deliberate way to outlive the terminal.
func signalSession(sid int, sig syscall.Signal) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		_ = syscall.Kill(-sid, sig)
		return
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if s, ok := sessionOf(pid); ok && s == sid {
			_ = syscall.Kill(pid, sig)
		}
	}
}

// sessionOf reads the session id from /proc/<pid>/stat. The command name in field 2 may hold
// spaces and parentheses, so fields are counted from the last ')'.
func sessionOf(pid int) (int, bool) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return 0, false
	}
	// After ")": state, ppid, pgrp, session.
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 4 {
		return 0, false
	}
	sid, err := strconv.Atoi(fields[3])
	return sid, err == nil
}
