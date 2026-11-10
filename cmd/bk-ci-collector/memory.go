package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// This watchdog supplements (not replaces) OS hard limits. On macOS there is
// no Job Object/cgroup equivalent and this is a best-effort self RSS bound.
func watchMemory(ctx context.Context) {
	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return
	}
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			info, err := p.MemoryInfoWithContext(ctx)
			if err == nil && info.RSS > 256<<20 {
				log.Print("E! collector exceeded 256 MiB RSS budget")
				terminateTree()
				os.Exit(1)
			}
		}
	}
}
