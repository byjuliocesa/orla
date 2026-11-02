package cli

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func startMarked(t *testing.T, run string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.Env = append(os.Environ(), runEnv+"="+run)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

func TestKillLeftoversKillsOnlyThatRun(t *testing.T) {
	target := startMarked(t, "run_crashed")
	// An id that merely starts with the crashed one belongs to another run.
	other := startMarked(t, "run_crashed2")

	if n := KillLeftovers("run_crashed"); n != 1 {
		t.Fatalf("killed %d processes, want 1", n)
	}

	done := make(chan error, 1)
	go func() { done <- target.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the crashed run's process survived")
	}
	if err := syscall.Kill(other.Process.Pid, 0); err != nil {
		t.Fatalf("another run's process was killed: %v", err)
	}
}
