//go:build !windows && !linux

package main

import (
	"os"
	"syscall"
)

func terminateTree() { _ = syscall.Kill(-os.Getpid(), syscall.SIGKILL) }
