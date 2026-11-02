package cli

import (
	"bytes"
	"os"
	"strconv"
	"syscall"
)

// sysProcAttr puts the CLI in its own process group, so cancelling reaches the tools it
// started, and has the kernel kill it if Orla dies.
//
// Pdeathsig fires when the thread that started the child exits, not the process. Go keeps
// threads alive unless a goroutine locked to one exits, which Orla does not do; KillLeftovers
// covers the rest after a restart.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

// KillLeftovers kills processes still running for a run whose Orla process died: the CLI if
// Pdeathsig missed it, and the tools it started, which Pdeathsig never covers. They are found by
// the ORLA_RUN variable every run sets and children inherit, so a reused pid is never mistaken
// for one of them. It returns how many it signalled.
func KillLeftovers(run string) int {
	marker := []byte(runEnv + "=" + run + "\x00")
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	killed := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		env, err := os.ReadFile("/proc/" + e.Name() + "/environ")
		if err != nil {
			continue
		}
		if bytes.HasPrefix(env, marker) || bytes.Contains(env, append([]byte{0}, marker...)) {
			if syscall.Kill(pid, syscall.SIGKILL) == nil {
				killed++
			}
		}
	}
	return killed
}
