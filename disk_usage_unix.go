//go:build linux || darwin || freebsd || openbsd || netbsd

package main

import "golang.org/x/sys/unix"

// diskUsagePercent implements the disk usage percent helper.
func diskUsagePercent(path string) float64 {
	if path == "" {
		path = "."
	}
	// `stat` stores the value used by this operation.
	var stat unix.Statfs_t
	// `err` stores the error produced by this operation.
	if err := unix.Statfs(path, &stat); err != nil || stat.Blocks == 0 {
		return 0
	}
	// `used` stores the value produced by this operation.
	used := stat.Blocks - stat.Bfree
	return (float64(used) / float64(stat.Blocks)) * 100
}
