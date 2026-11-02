//go:build !linux

package cli

import "syscall"

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// KillLeftovers needs /proc to find a dead run's processes; elsewhere it finds none.
func KillLeftovers(run string) int { return 0 }
