//go:build !linux

package terminal

import "syscall"

// signalSession signals the session leader's process group. Without /proc there is no cheap way
// to find every process of the session, so background jobs of a shell with job control, which
// run in groups of their own, are missed here. The server targets Linux; this keeps the package
// building elsewhere.
func signalSession(sid int, sig syscall.Signal) {
	_ = syscall.Kill(-sid, sig)
}
