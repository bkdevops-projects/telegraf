//go:build linux

package external

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// runCommand puts a script in a child cgroup of the collector's cgroup when
// available, otherwise in its own process group. Either way timeout and
// completion kill the script together with its descendants.
func runCommand(cmd *exec.Cmd, environment []string) ([]byte, error) {
	cmd.Env = commandEnv(os.Environ(), environment)
	var stdout, stderr cappedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second

	cleanup, err := containScript(cmd)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	err = cmd.Run()
	if stdout.overflow || stderr.overflow {
		return nil, errors.New("script output exceeds 1 MiB")
	}
	return stdout.data, err
}

func containScript(cmd *exec.Cmd) (func(), error) {
	if parent := os.Getenv("BKCI_COMPONENT_CGROUP"); parent != "" {
		group := filepath.Join(parent, fmt.Sprintf("script-%d", time.Now().UnixNano()))
		if err := os.Mkdir(group, 0700); err == nil {
			f, err := os.Open(group)
			if err == nil {
				kill := func() error { return os.WriteFile(filepath.Join(group, "cgroup.kill"), []byte("1"), 0600) }
				cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, UseCgroupFD: true, CgroupFD: int(f.Fd())}
				cmd.Cancel = kill
				return func() { _ = kill(); f.Close(); _ = os.Remove(group) }, nil
			}
			_ = os.Remove(group)
		}
	}
	// Process-group fallback: no resource limits, but descendants are still killed.
	killGroup := func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = killGroup
	return func() { _ = killGroup() }, nil
}
