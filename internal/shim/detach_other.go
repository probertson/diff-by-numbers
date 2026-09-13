//go:build !unix

package shim

import "syscall"

// detachAttr is a no-op on platforms without process groups. dbn ships only unix
// builds; this exists so the package still compiles elsewhere for development.
func detachAttr() *syscall.SysProcAttr { return nil }
