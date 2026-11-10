//go:build windows

package main

// The parent's non-inheritable Job Object handle owns this entire process tree.
// Windows kills the job on parent exit; normal stop is handled by the supervisor.
func terminateTree() {}
