//go:build windows

package external

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func runCommand(cmd *exec.Cmd, environment []string) ([]byte, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(job)
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE}}
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED}
	cmd.Env = commandEnv(os.Environ(), environment)
	var stdout, stderr cappedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return windows.TerminateJobObject(job, 1) }
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	defer windows.TerminateJobObject(job, 1)
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, uint32(cmd.Process.Pid))
	if err == nil {
		defer windows.CloseHandle(h)
		err = windows.AssignProcessToJobObject(job, h)
		if err == nil {
			status, _, _ := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess").Call(uintptr(h))
			if status != 0 {
				err = fmt.Errorf("resume script status %#x", status)
			}
		}
	}
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, err
	}
	err = cmd.Wait()
	if stdout.overflow || stderr.overflow {
		return nil, errors.New("script output exceeds 1 MiB")
	}
	return stdout.data, err
}
