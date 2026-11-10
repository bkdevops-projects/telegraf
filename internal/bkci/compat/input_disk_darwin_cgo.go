//go:build darwin && cgo && !loong64
// +build darwin,cgo,!loong64

package monitor

import "github.com/influxdata/telegraf/internal/bkci/diskutil"

// darwinAvailableCapacity delegates to the shared diskutil package which
// uses macOS Foundation API to get available capacity including purgeable space.
func darwinAvailableCapacity(path string) (uint64, bool) {
	return diskutil.AvailableCapacity(path)
}
