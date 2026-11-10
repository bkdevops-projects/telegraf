//go:build !windows && !linux

package external

import (
	"errors"
	"os/exec"
)

func runCommand(*exec.Cmd, []string) ([]byte, error) {
	return nil, errors.New("bounded script execution is unsupported on this platform")
}
