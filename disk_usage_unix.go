//go:build linux || darwin || freebsd || openbsd || netbsd

package main

import "golang.org/x/sys/unix"

func diskUsagePercent(path string) float64 {
	if path == "" {
		path = "."
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil || stat.Blocks == 0 {
		return 0
	}
	used := stat.Blocks - stat.Bfree
	return (float64(used) / float64(stat.Blocks)) * 100
}
