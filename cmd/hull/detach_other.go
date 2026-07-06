//go:build !windows

package main

import "syscall"

// detachedSysProcAttr puts the child in its own session so it outlives the CLI.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
