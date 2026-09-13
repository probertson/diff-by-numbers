//go:build unix

package shim

import "syscall"

// detachAttr puts the spawned daemon in its own process group, so a signal sent
// to the shim's group (or the shim's own death) does not take the daemon with it.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
