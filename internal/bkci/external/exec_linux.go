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

func runCommand(cmd *exec.Cmd, environment []string) ([]byte, error) {
	parent := os.Getenv("BKCI_COMPONENT_CGROUP")
	if parent == "" {
		return nil, errors.New("script input requires component cgroup")
	}
	group := filepath.Join(parent, fmt.Sprintf("script-%d", time.Now().UnixNano()))
	if err := os.Mkdir(group, 0700); err != nil {
		return nil, err
	}
	defer os.Remove(group)
	f, err := os.Open(group)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	kill := func() error { return os.WriteFile(filepath.Join(group, "cgroup.kill"), []byte("1"), 0600) }
	defer kill()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, UseCgroupFD: true, CgroupFD: int(f.Fd())}
	cmd.Env = commandEnv(os.Environ(), environment)
	var stdout, stderr cappedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	cmd.Cancel = kill
	err = cmd.Run()
	if stdout.overflow || stderr.overflow {
		return nil, errors.New("script output exceeds 1 MiB")
	}
	return stdout.data, err
}
