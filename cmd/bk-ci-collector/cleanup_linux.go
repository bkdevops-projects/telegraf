//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func terminateTree() {
	group := os.Getenv("BKCI_COMPONENT_CGROUP")
	if group != "" && strings.HasPrefix(filepath.Base(group), "bkci-component-") {
		// Includes this process. Used only after cancellation/normal completion.
		_ = os.WriteFile(filepath.Join(group, "cgroup.kill"), []byte("1"), 0600)
	}
	// Group cleanup remains a fallback on kernels without cgroup.kill.
	_ = syscall.Kill(-os.Getpid(), syscall.SIGKILL)
}
